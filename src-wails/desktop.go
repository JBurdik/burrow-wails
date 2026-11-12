//go:build !headless

package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"burrow/internal/agentphase"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
)

// The native window side of the app. Everything here needs a Wails window and
// so has a no-op twin in main_headless.go; the rest of the backend never
// imports Wails.
var (
	wapp    *application.App
	mainWin *application.WebviewWindow
)

// macTitleBar is HiddenInset with the compact unified toolbar. Plain
// HiddenInset gets the tall unified toolbar on recent macOS, which drops the
// traffic lights below our titlebar icons; compact puts them back in line.
var macTitleBar = func() application.MacTitleBar {
	t := application.MacTitleBarHiddenInset
	t.ToolbarStyle = application.MacToolbarStyleUnifiedCompact
	return t
}()

// desktopEmit sends a desktop-only event straight to the native window. Only
// the files on events_test.go's allowlist may call it — everything else goes
// through busEmit so remote clients see it too.
func desktopEmit(name string, data ...any) {
	if wapp != nil {
		wapp.Event.Emit(name, data...)
	}
}

func desktopQuit() {
	if wapp != nil {
		wapp.Quit()
	}
}

// pickPaths backs PickDirectory/PickFile/PickFiles (dialog.go).
func pickPaths(dirs, multiple bool, filterName string, filterExts []string) ([]string, error) {
	d := wapp.Dialog.OpenFile().CanChooseFiles(!dirs).CanChooseDirectories(dirs).CanCreateDirectories(dirs)
	if filterName != "" && len(filterExts) > 0 {
		d.AddFilter(filterName, "*."+strings.Join(filterExts, ";*."))
	}
	if multiple {
		return d.PromptForMultipleSelection()
	}
	p, err := d.PromptForSingleSelection()
	if p == "" {
		return nil, err
	}
	return []string{p}, err
}

// DesktopService is the whole Wails binding surface: what the desktop calls
// in-process instead of over /v2/ws. LocalEndpoint is the door to the socket
// (being in-process is the desktop's authorization); the rest is native
// dialogs, the updater and the float-snapshot relay. Everything else lives
// on App behind remoteAllowed.
//
// ui: the backend is an external core (plan 009); s.a is then a bare App
// that was never started — fine for the stateless methods below, while
// LocalEndpoint asks the core for a ticket instead.
type DesktopService struct {
	a  *App
	ui bool
}

func (s *DesktopService) LocalEndpoint() LocalEndpointInfo {
	if !s.ui {
		return s.a.LocalEndpoint()
	}
	info, err := ensureCore()
	if err != nil {
		log.Printf("ui: %v", err)
	}
	return info // empty on failure: the transport retries with a fresh call
}
func (s *DesktopService) GetAppVersion() string          { return s.a.GetAppVersion() }
func (s *DesktopService) PickDirectory() (string, error) { return s.a.PickDirectory() }
func (s *DesktopService) PickFile(filterName string, filterExts []string) (string, error) {
	return s.a.PickFile(filterName, filterExts)
}
func (s *DesktopService) PickFiles(filterName string, filterExts []string) ([]string, error) {
	return s.a.PickFiles(filterName, filterExts)
}
func (s *DesktopService) CheckUpdate() (UpdateInfo, error)    { return s.a.CheckUpdate() }
func (s *DesktopService) InstallUpdate(url, sum string) error { return s.a.InstallUpdate(url, sum) }
func (s *DesktopService) InstallAgentDocs()                   { s.a.InstallAgentDocs() }
func (s *DesktopService) RelaunchApp() error                  { return s.a.RelaunchApp() }
func (s *DesktopService) SendFloatSnapshot(ptyID, data string, cols, rows int) error {
	return s.a.SendFloatSnapshot(ptyID, data, cols, rows)
}
func (s *DesktopService) NotifyFloatGrid(ptyID string, cols, rows int) error {
	return s.a.NotifyFloatGrid(ptyID, cols, rows)
}

// --- Secondary windows (Settings, popped-out tabs, the git panel) ---
//
// Each is the same frontend loaded at a "/window/..." hash route, which
// main.ts boots into WindowApp.vue instead of App.vue. key makes a window
// single-instance: opening it again only brings it forward. Closing one
// emits window-closed <key> so the main window can take a popped tab back.

var (
	extraWins = map[string]*application.WebviewWindow{}
	onTop     = map[string]bool{}
	extraMu   sync.Mutex
)

// floating disables the zoom/fullscreen button: tiling window managers
// (AeroSpace's isDialogHeuristic) treat such a window as a dialog and float
// it instead of tiling it, as System Settings is — right for Settings, while
// a popped-out terminal is better tiled.
func (s *DesktopService) OpenWindow(key, route, title string, width, height int, floating bool) {
	extraMu.Lock()
	defer extraMu.Unlock()
	if w, ok := extraWins[key]; ok {
		w.Show()
		w.Focus()
		return
	}
	zoom := application.ButtonEnabled
	if floating {
		zoom = application.ButtonDisabled
	}
	w := wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                key,
		MaximiseButtonState: zoom,
		InitialPosition:     application.WindowCentered,
		Title:               title,
		Width:               width,
		Height:              height,
		URL:                 "/#" + route,
		BackgroundColour:    application.NewRGB(27, 38, 54),
		Mac:                 application.MacWindow{TitleBar: macTitleBar},
	})
	extraWins[key] = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		extraMu.Lock()
		delete(extraWins, key)
		delete(onTop, key)
		extraMu.Unlock()
		desktopEmit("window-closed", key)
	})
}

// CloseWindow closes a secondary window from the main one ("bring back").
// TrayOpen is the tray panel's click: hide the panel, bring the main window
// forward and route it (App.vue listens on menu-navigate).
func (s *DesktopService) TrayOpen(route string) {
	if trayWin != nil && !trayPinned.Load() {
		trayWin.Hide()
	}
	if mainWin == nil {
		openUI(route)
		return
	}
	showMainWindow()
	if route != "" {
		desktopEmit("menu-navigate", route)
	}
}

// TraySetPinned turns the tray panel into a free-standing always-on-top panel:
// it stays up when it loses focus, resizes, and a tray click shows it where
// the user left it instead of snapping it back under the icon. Not persisted.
// Unpinning puts it back under the icon (the client resizes it first).
func (s *DesktopService) TraySetPinned(pinned bool) {
	trayPinned.Store(pinned)
	if trayWin == nil {
		return
	}
	trayWin.SetResizable(pinned)
	setTrayPanelMovable(trayWin, pinned)
	if !pinned && trayWin.IsVisible() {
		_ = tray.PositionWindow(trayWin, trayOffset)
	}
}

const trayOffset = 6

// showTrayPanel orders the panel in WITHOUT key status, then focuses it a
// beat later. AeroSpace classifies a new window once, when it first sees it:
// a buttonless, untitled, unfocused non-standard window is a popup, which it
// leaves alone on every workspace; the same window seen focused becomes a
// floating window bound to one workspace and is hidden on the next switch.
// The panel is non-activating, so Show() is orderFrontRegardless (no key).
// ponytail: 250 ms is a guess at AeroSpace's reaction time, not a contract.
func showTrayPanel(underIcon bool) {
	hideTrayToasts() // the panel lists the same threads, over the same spot
	if underIcon {
		_ = tray.PositionWindow(trayWin, trayOffset)
	}
	trayWin.Show()
	time.AfterFunc(250*time.Millisecond, func() {
		if trayWin.IsVisible() {
			trayWin.Focus()
		}
	})
}

// TraySetHotkey re-registers the tray panel's global shortcut after Settings
// changed uiPrefs.trayHotkey ("" = none). Returns why it failed, "" if not.
func (s *DesktopService) TraySetHotkey(spec string) string {
	if err := setTrayHotkey(spec); err != nil {
		return err.Error()
	}
	return ""
}

// ShowMain brings the main window forward: the core's ui-open (App.vue).
func (s *DesktopService) ShowMain() {
	if mainWin != nil {
		mainWin.Show()
		mainWin.Focus()
	}
}

// QuitApp quits this process: the UI's answer to the core's core-quit.
func (s *DesktopService) QuitApp() {
	quitting.Store(true)
	if mainWin != nil {
		s.a.saveWindowState(mainWin)
	}
	wapp.Quit()
}

// SetDockBadge shows n (0 = none) on the Dock icon. In the UI role the
// count comes from the core's waiting-count; in-process refreshTray does it.
func (s *DesktopService) SetDockBadge(n int) {
	if !s.ui || !started.Load() {
		return
	}
	trayMu.Lock()
	defer trayMu.Unlock()
	setDockBadge(n)
}

// CoreNotifies: native notifications come from the external core, so the
// webview must not post its own (it still toasts).
func (s *DesktopService) CoreNotifies() bool { return s.ui }

// ShowGrantHelper opens System Settings at pane (an x-apple.systempreferences
// URL) and floats path's icon beside it to drag into the list; Close hides it.
func (s *DesktopService) ShowGrantHelper(path, pane, title, body string, theme GrantTheme) {
	showGrantHelper(path, pane, title, body, theme)
}
func (s *DesktopService) CloseGrantHelper() { closeGrantHelper() }

// OpenSystemSettings opens an x-apple.systempreferences URL (or any URL).
func (s *DesktopService) OpenSystemSettings(url string) {
	if strings.HasPrefix(url, "x-apple.systempreferences:") {
		_ = exec.Command("/usr/bin/open", url).Start()
	}
}

// ExternalCore: the backend runs as its own process (dev/beta, plan 009).
func (s *DesktopService) ExternalCore() bool { return s.ui }

// CoreAtLogin reads ("" op) or sets ("on" / "off") the core's login item and
// returns its state: on · off · needs-approval (System Settings → Login
// Items) · unavailable · unsupported · error: <why>.
func (s *DesktopService) CoreAtLogin(op string) string {
	code := map[string]int{"on": 1, "off": 2}[op]
	st, err := coreLoginItem(code)
	if err != nil {
		return "error: " + err.Error()
	}
	return st
}

func (s *DesktopService) CloseWindow(key string) {
	extraMu.Lock()
	w := extraWins[key]
	extraMu.Unlock()
	if w != nil {
		w.Close()
	}
}

// openMiniChat opens (or brings forward) the mini chat window from Go — the
// core's tray menu. The UI process asks the same thing over the wire
// (mini_chat) and opens the window itself (src/lib/miniChat.ts).
func openMiniChat(a *App, fresh bool) {
	m, err := a.MiniChat(fresh)
	if err != nil {
		log.Printf("mini chat: %v", err)
		return
	}
	new(DesktopService).OpenWindow(miniChatWinKey, m["route"].(string), miniChatTitle, 420, 600, true)
}

// toggleKeepOnTop backs Window → Keep on Top for whichever window is focused.
func toggleKeepOnTop() {
	w, ok := wapp.Window.Current().(*application.WebviewWindow)
	if !ok || w == nil {
		return
	}
	extraMu.Lock()
	on := !onTop[w.Name()]
	onTop[w.Name()] = on
	extraMu.Unlock()
	w.SetAlwaysOnTop(on)
}

// Window geometry: restored once the app has started, saved on close and ⌘Q.

func (a *App) restoreWindowState(w *application.WebviewWindow) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return // first launch (or unreadable/empty) — keep the defaults from main.go
	}
	var st windowState
	if err := json.Unmarshal(b, &st); err != nil {
		log.Printf("window state: %v", err)
		return
	}
	if st.Width >= 400 && st.Height >= 300 {
		w.SetSize(st.Width, st.Height)
	}
	// ponytail: sanity bound only — the Wails Screen struct exposes no screen
	// origin, so a real "is this rect on a connected display" clamp isn't
	// possible here. macOS keeps the titlebar reachable in practice; if a
	// detached-monitor case bites, clamp against ScreenGetAll once origins land.
	if abs(st.X) < 20000 && abs(st.Y) < 20000 && (st.X != 0 || st.Y != 0) {
		w.SetPosition(st.X, st.Y)
	}
	if st.Maximised {
		w.Maximise()
	}
}

func (a *App) saveWindowState(w *application.WebviewWindow) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	st := windowState{Maximised: w.IsMaximised()}
	st.X, st.Y = w.Position()
	st.Width, st.Height = w.Size()
	abnormal := st.Maximised || w.IsFullscreen() || w.IsMinimised()
	var prev windowState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &prev)
	}
	st = mergeWindowState(prev, st, abnormal)
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Printf("window state: %v", err)
	}
}

// --- Hide on close + system tray + Dock badge ---
//
// Closing the window only hides it: the backend keeps serving the phone,
// schedules and running chats. ⌘Q / the tray's Quit really quit. The tray
// icon (a template image, so macOS tints it for a light or dark menu bar) and
// the Dock badge both show how many chats and tabs are waiting on the user.

//go:embed build/trayicon.png
var trayIcon []byte

var (
	quitting   atomic.Bool
	tray       *application.SystemTray
	trayWin    *application.WebviewWindow
	trayPinned atomic.Bool
	dockBadge  = dock.New()
	trayMu     sync.Mutex
	started    atomic.Bool // run loop up: the badge call dispatch_syncs onto the main queue
	lastBadged int
)

func showMainWindow() {
	if mainWin != nil {
		mainWin.Show()
		mainWin.Focus()
		return
	}
	openUI("") // the core role: the window is another process
}

func quitApp(a *App) {
	quitting.Store(true)
	if mainWin != nil {
		a.saveWindowState(mainWin)
	} else {
		// The core role. Tell the UI first: it would otherwise see the socket
		// drop and start a fresh core. Quit = both (plan 009).
		busEmit("core-quit", nil)
		time.Sleep(300 * time.Millisecond)
	}
	wapp.Quit()
}

func setupTray(a *App) {
	tray = wapp.SystemTray.New()
	tray.SetTemplateIcon(trayIcon)
	m := application.NewMenu()
	m.Add("Open Burrow").OnClick(func(*application.Context) { showMainWindow() })
	m.Add("Mini Chat").OnClick(func(*application.Context) { go openMiniChat(a, false) })
	m.AddSeparator()
	m.Add("Quit Burrow").OnClick(func(*application.Context) { quitApp(a) })
	tray.SetMenu(m) // right click; left click toggles the panel below
	// The tray panel: a frameless, translucent window under the icon running
	// WindowApp's "tray" kind. Hidden on focus loss, like a menu-bar popover.
	trayWin = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "tray",
		Title:            "", // AeroSpace's popup heuristic wants an empty (or "Window") title
		Width:            340,
		Height:           440,
		MinWidth:         300,
		MinHeight:        120,
		Hidden:           true,
		Frameless:        true,
		DisableResize:    true,
		URL:              "/#/window/tray",
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		// An NSPanel, not a window: tiling WMs (AeroSpace, yabai) only manage
		// AXStandardWindow, so a panel is never tiled. Pop-up-menu level, on
		// whichever Space (or fullscreen app) is current, out of ⌘` cycling.
		Mac: application.MacWindow{
			Backdrop:    application.MacBackdropTranslucent,
			WindowClass: application.MacWindowClassPanel,
			// NonActivating: showing it neither activates Burrow nor makes it
			// key (see showTrayPanel); the composer still takes keys once focused.
			PanelPreferences: application.MacPanelPreferences{FloatingPanel: true, NonActivating: true},
			WindowLevel:      application.MacWindowLevelPopUpMenu,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorTransient |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
	})
	trayWin.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		if !trayPinned.Load() {
			trayWin.Hide()
		}
	})
	trayWin.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !quitting.Load() {
			e.Cancel()
			trayWin.Hide()
		}
	})
	tray.AttachWindow(trayWin).WindowOffset(trayOffset)
	if runtime.GOOS == "darwin" {
		setupTrayToast()
	}
	// Unpinned: the stock toggle (re-positions under the icon). Pinned: toggle
	// in place, so a panel the user dragged elsewhere stays there.
	tray.OnClick(func() {
		if trayWin.IsVisible() {
			trayWin.Hide()
			return
		}
		showTrayPanel(!trayPinned.Load())
	})
	refreshTray(a)
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		started.Store(true)
		go func() { // InvokeSync: never from the main thread
			prepareTrayPanel(trayWin)
			// The client may have restored a pin before this ran (TrayPanel's
			// restoreGeometry): prepare's movable=NO must not undo it.
			setTrayPanelMovable(trayWin, trayPinned.Load())
			if toastWin != nil {
				prepareTrayToast(toastWin)
			}
			registerTrayHotkey(a)
		}()
		go refreshTray(a)
	})
}

// refreshTray recounts from PhaseStore.All rather than tracking deltas, so a
// Forget()ed phase (reused PTY id) can't leave a stale count behind. Called
// on its own goroutine from onChange: All takes the store's lock, and the
// Dock badge blocks on the main thread.
func refreshTray(a *App) {
	if tray == nil || a.phases == nil {
		return
	}
	trayMu.Lock()
	defer trayMu.Unlock()
	n := waitingCount(a.phases.All())
	tray.SetLabel(trayLabel(n))
	if mainWin == nil { // the core role: the Dock icon belongs to the UI process
		if n != lastBadged {
			lastBadged = n
			busEmit("waiting-count", n)
		}
		return
	}
	if started.Load() {
		setDockBadge(n)
	}
}

// setDockBadge, under trayMu: the badge call dispatch_syncs onto the main
// queue, so only ever from a goroutine after ApplicationStarted.
func setDockBadge(n int) {
	if n == lastBadged {
		return
	}
	var err error
	if n == 0 {
		err = dockBadge.RemoveBadge()
	} else {
		err = dockBadge.SetBadge(strconv.Itoa(n))
	}
	if err != nil {
		log.Printf("dock badge: %v", err)
		return
	}
	lastBadged = n
}

// WaitingCount is the tray / Dock count: chats and tabs waiting on the user.
func (a *App) WaitingCount() int {
	if a.phases == nil {
		return 0
	}
	return waitingCount(a.phases.All())
}

func waitingCount(phases map[string]agentphase.Phase) int {
	n := 0
	for _, p := range phases {
		if p.State == agentphase.WaitingInput || p.State == agentphase.WaitingApproval {
			n++
		}
	}
	return n
}

// trayLabel is the text beside the icon: the count, or nothing.
func trayLabel(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func desktopPhaseChanged(a *App) { go refreshTray(a) }

// --- Tray toasts ---
//
// While the main window is not focused, pushFor (pushtrigger.go) drops a
// toast from the tray icon instead of posting an OS notification. Its own
// window (WindowApp kind "traytoast", TrayToast.vue), not part of the tray
// panel: the panel hides on focus loss, sizes itself to its list and may be
// pinned elsewhere — a toast has none of that, and the two never show at once.
// Same NSPanel recipe as the panel (non-activating, pop-up level, all Spaces)
// but it is NEVER focused: AeroSpace sees it unfocused, buttonless and
// untitled, i.e. a popup it leaves alone, and the user's app keeps the keys.

const trayToastWidth = 340

var (
	toastWin   *application.WebviewWindow
	toastReady atomic.Bool // TrayToast.vue mounted and listening
)

func setupTrayToast() {
	toastWin = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "traytoast",
		Title:            "",
		Width:            trayToastWidth,
		Height:           90,
		Hidden:           true,
		Frameless:        true,
		DisableResize:    true,
		URL:              "/#/window/traytoast",
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Mac: application.MacWindow{
			Backdrop:         application.MacBackdropTransparent,
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{FloatingPanel: true, NonActivating: true},
			WindowLevel:      application.MacWindowLevelPopUpMenu,
			CollectionBehavior: application.MacWindowCollectionBehaviorCanJoinAllSpaces |
				application.MacWindowCollectionBehaviorTransient |
				application.MacWindowCollectionBehaviorFullScreenAuxiliary |
				application.MacWindowCollectionBehaviorIgnoresCycle,
		},
	})
	toastWin.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !quitting.Load() {
			e.Cancel()
			toastWin.Hide()
		}
	})
}

func trayToastReady() bool { return toastWin != nil && toastReady.Load() }

// showTrayToast hands t to the toast window; false = could not (no window
// yet, or not listening), so the caller falls back to an OS notification.
// While the tray panel is up the toast is dropped but counts as shown: the
// panel already lists that thread.
func showTrayToast(t trayToast) bool {
	if !trayToastReady() {
		return false
	}
	if trayWin != nil && trayWin.IsVisible() {
		return true
	}
	desktopEmit("tray-toast", t)
	return true
}

func hideTrayToasts() {
	if toastWin != nil && toastWin.IsVisible() {
		desktopEmit("tray-toast-clear")
		toastWin.Hide()
	}
}

// mainWindowFocused: known only in-process; with an external core the window
// belongs to another process.
func mainWindowFocused() (focused, known bool) {
	if mainWin == nil {
		return false, false
	}
	return mainWin.IsFocused(), true
}

// TrayToastReady: TrayToast.vue is listening for tray-toast.
func (s *DesktopService) TrayToastReady() { toastReady.Store(true) }

// TrayToastLayout sizes the toast window to its stack (CSS px × zoom) and
// shows it under the tray icon, without focus; 0 hides it.
func (s *DesktopService) TrayToastLayout(height int) {
	if toastWin == nil || tray == nil {
		return
	}
	if height <= 0 {
		toastWin.Hide()
		return
	}
	toastWin.SetSize(trayToastWidth, height)
	_ = tray.PositionWindow(toastWin, trayOffset)
	if !toastWin.IsVisible() {
		toastWin.Show() // non-activating panel: orderFrontRegardless, never key
	}
}

// TrayToasts: Go tells an away user itself (tray toast, OS notification as
// fallback), so the webview must not post its own OS notification.
// Both roles: in the UI role the core (same Mac) owns the tray.
func (s *DesktopService) TrayToasts() bool { return runtime.GOOS == "darwin" }
