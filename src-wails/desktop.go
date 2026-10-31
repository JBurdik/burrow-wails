//go:build !headless

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"burrow/internal/agentphase"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The native window side of the app. Everything here needs a Wails window and
// so has a no-op twin in main_headless.go; the rest of the backend never
// imports Wails.
var (
	wapp    *application.App
	mainWin *application.WebviewWindow
)

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
type DesktopService struct{ a *App }

func (s *DesktopService) LocalEndpoint() LocalEndpointInfo { return s.a.LocalEndpoint() }
func (s *DesktopService) GetAppVersion() string            { return s.a.GetAppVersion() }
func (s *DesktopService) PickDirectory() (string, error)   { return s.a.PickDirectory() }
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
func (s *DesktopService) OpenGitPanelWindow() error           { return s.a.OpenGitPanelWindow() }
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
		Mac:                 application.MacWindow{TitleBar: application.MacTitleBarHiddenInset},
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
func (s *DesktopService) CloseWindow(key string) {
	extraMu.Lock()
	w := extraWins[key]
	extraMu.Unlock()
	if w != nil {
		w.Close()
	}
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
	if err != nil {
		return // first launch (or unreadable) — keep the defaults from main.go
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

// --- Hide on close + system tray ---
//
// Closing the window only hides it: the backend keeps serving the phone,
// schedules and running chats. ⌘Q / the tray's Quit really quit. The tray
// shows how many chats and tabs are waiting on the user.

var (
	quitting atomic.Bool
	tray     *application.SystemTray
	trayMu   sync.Mutex
)

func showMainWindow() {
	if mainWin != nil {
		mainWin.Show()
		mainWin.Focus()
	}
}

func quitApp(a *App) {
	quitting.Store(true)
	a.saveWindowState(mainWin)
	wapp.Quit()
}

func setupTray(a *App) {
	tray = wapp.SystemTray.New()
	m := application.NewMenu()
	m.Add("Open Burrow").OnClick(func(*application.Context) { showMainWindow() })
	m.AddSeparator()
	m.Add("Quit Burrow").OnClick(func(*application.Context) { quitApp(a) })
	tray.SetMenu(m)
	refreshTray(a)
}

// refreshTray recounts from PhaseStore.All rather than tracking deltas, so a
// Forget()ed phase (reused PTY id) can't leave a stale count behind. Called
// on its own goroutine from onChange: All takes the store's lock.
func refreshTray(a *App) {
	if tray == nil || a.phases == nil {
		return
	}
	trayMu.Lock()
	defer trayMu.Unlock()
	tray.SetLabel(trayLabel(a.phases.All()))
}

// ponytail: text label, no icon — a template icon can come with the Dock badge.
func trayLabel(phases map[string]agentphase.Phase) string {
	n := 0
	for _, p := range phases {
		if p.State == agentphase.WaitingInput || p.State == agentphase.WaitingApproval {
			n++
		}
	}
	if n == 0 {
		return "◆"
	}
	return fmt.Sprintf("◆ %d", n)
}

func desktopPhaseChanged(a *App) { go refreshTray(a) }
