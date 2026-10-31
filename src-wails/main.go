//go:build !headless

package main

import (
	"context"
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}

	wapp = application.New(application.Options{
		Name:     "Burrow",
		Services: []application.Service{application.NewService(&DesktopService{a: app})},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		// ⌘Q never fires WindowClosing, so the geometry is saved here too.
		ShouldQuit: func() bool {
			quitting.Store(true)
			app.saveWindowState(mainWin)
			return true
		},
		OnShutdown: app.cleanupOnShutdown,
	})
	wapp.Menu.SetApplicationMenu(appMenu())

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
		Mac:            application.MacWindow{TitleBar: application.MacTitleBarHiddenInset},
	})
	mainWin.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		x, y := 0, 0
		if d := e.Context().DropTargetDetails(); d != nil {
			x, y = d.X, d.Y
		}
		busEmit("file-drop", map[string]any{"x": x, "y": y, "paths": e.Context().DroppedFiles()})
	})
	// Close = hide; the backend keeps running for the phone and schedules.
	mainWin.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		app.saveWindowState(mainWin)
		if !quitting.Load() {
			e.Cancel()
			mainWin.Hide()
		}
	})
	wapp.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		showMainWindow()
	})
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.restoreWindowState(mainWin)
	})

	app.startup(context.Background())
	setupTray(app)

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}

func appMenu() *application.Menu {
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
	am.AddRole(application.Quit)
	m.AddRole(application.EditMenu)
	m.AddRole(application.WindowMenu)
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
