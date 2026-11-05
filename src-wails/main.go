//go:build !headless

package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	setupAppLog()
	// Plan 009: dev/beta run the backend as `Burrow --core`; this process is
	// then only the windows (ui = true) and never starts App.
	if externalCore() && coreRoleRequested() {
		runCore()
		return
	}
	ui := externalCore()
	app := NewApp()
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}

	wapp = application.New(application.Options{
		Name:     "Burrow",
		Services: []application.Service{application.NewService(&DesktopService{a: app, ui: ui})},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		// ⌘Q never fires WindowClosing, so the geometry is saved here too.
		ShouldQuit: func() bool {
			quitting.Store(true)
			app.saveWindowState(mainWin)
			return true
		},
		OnShutdown: func() {
			if !ui { // the external core outlives this process
				app.cleanupOnShutdown()
			}
		},
	})
	wapp.Menu.SetApplicationMenu(appMenu(ui))

	mainWin = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "burrow",
		Width:            1024,
		Height:           768,
		BackgroundColour: application.NewRGB(27, 38, 54),
		// Real paths for files dragged in from Finder. v3 only reports a drop
		// that lands on a [data-file-drop-target] element — index.html puts
		// it on #app, and the frontend hit-tests x/y against the chat
		// composer itself (AgentChat.vue).
		EnableFileDrop: true,
		Mac:            application.MacWindow{TitleBar: macTitleBar},
		URL:            startURL(),
	})
	mainWin.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		x, y := 0, 0
		if d := e.Context().DropTargetDetails(); d != nil {
			x, y = d.X, d.Y
		}
		desktopEmit("file-drop", map[string]any{"x": x, "y": y, "paths": e.Context().DroppedFiles()})
	})
	// In-process: close = hide; the backend keeps running for the phone and
	// schedules. With an external core the backend is elsewhere, so closing
	// the window quits this process (plan 009) — popouts go with it.
	mainWin.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		app.saveWindowState(mainWin)
		if quitting.Load() {
			return
		}
		e.Cancel()
		if ui {
			quitting.Store(true)
			go wapp.Quit()
			return
		}
		mainWin.Hide()
	})
	wapp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		showMainWindow()
	})
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.restoreWindowState(mainWin)
	})

	if ui {
		go watchUIBinary()
		// Start (or find) the core now rather than on the webview's first
		// ticket request, so a cold core boots while the window loads.
		go func() {
			if _, err := ensureCore(); err != nil {
				log.Printf("ui: %v", err)
			}
		}()
	} else {
		app.startup(context.Background())
		setupTray(app) // in-process only: on dev/beta the core owns the tray
	}

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}

// startURL: a UI the core started for a tray / notification click opens at
// that route; anything else at the app's root.
func startURL() string {
	if r := uiStartRoute(); r != "" {
		return "/#" + r
	}
	return "/"
}

func appMenu(ui bool) *application.Menu {
	m := application.NewMenu()
	// The first submenu is the macOS app menu; built by hand (not the AppMenu
	// role) so "Check for Updates…" can sit under About.
	am := m.AddSubmenu("Burrow")
	am.AddRole(application.About)
	am.Add("Check for Updates…").OnClick(func(*application.Context) { desktopEmit("menu-check-update") })
	am.AddSeparator()
	am.AddRole(application.ServicesMenu)
	am.AddSeparator()
	am.AddRole(application.Hide)
	am.AddRole(application.HideOthers)
	am.AddRole(application.UnHide)
	am.AddSeparator()
	// Not the Quit role: ⌘Q goes through hold-to-quit (quithold.go). The
	// tray's Quit and ⌥⌘Q below still quit at once; the Dock's Quit and a
	// logout reach ShouldQuit directly.
	hold := &quitHold{
		emit:  func(state string) { desktopEmit("menu-quit-hold", state) },
		quit:  func() { wapp.Quit() },
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
	}
	am.Add("Quit Burrow").SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) {
		if !holdToQuitPref() {
			wapp.Quit()
			return
		}
		hold.press(time.Now())
	})
	if ui {
		// ⌘Q quits only this window process; the core goes on while it has
		// work (coreidle.go). This one stops the core too.
		am.Add("Quit Burrow Completely").SetAccelerator("CmdOrCtrl+Alt+Q").OnClick(func(*application.Context) {
			askCoreToQuit()
			quitting.Store(true)
			wapp.Quit()
		})
	}
	m.AddRole(application.EditMenu)
	wm := m.AddSubmenu("Window")
	wm.AddRole(application.Minimise)
	wm.AddRole(application.Zoom)
	wm.AddSeparator()
	wm.Add("Keep on Top").SetAccelerator("CmdOrCtrl+Alt+T").OnClick(func(*application.Context) { toggleKeepOnTop() })
	help := m.AddSubmenu("Help")
	for _, it := range []struct{ label, event string }{
		{"Documentation", "menu-open-docs"},
		{"What's New", "menu-whats-new"},
		{"Show Onboarding Again", "menu-show-onboarding"},
		{"Keyboard Shortcuts", "menu-show-shortcuts"},
	} {
		help.Add(it.label).OnClick(func(*application.Context) { desktopEmit(it.event) })
	}
	return m
}
