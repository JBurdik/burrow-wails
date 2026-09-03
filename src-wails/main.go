package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()
	appMenu := menu.NewMenu()
	burrowMenu := appMenu.AddSubmenu("Burrow")
	burrowMenu.AddText("Check for Updates…", nil, func(_ *menu.CallbackData) {
		runtime.EventsEmit(app.ctx, "menu-check-update")
	})
	appMenu.Append(menu.EditMenu())
	appMenu.Append(menu.WindowMenu())
	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("Documentation", nil, func(_ *menu.CallbackData) {
		runtime.EventsEmit(app.ctx, "menu-open-docs")
	})
	helpMenu.AddText("Show Onboarding Again", nil, func(_ *menu.CallbackData) {
		runtime.EventsEmit(app.ctx, "menu-show-onboarding")
	})
	helpMenu.AddText("Keyboard Shortcuts", nil, func(_ *menu.CallbackData) {
		runtime.EventsEmit(app.ctx, "menu-show-shortcuts")
	})

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "burrow",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnDomReady:       app.restoreWindowState,
		OnBeforeClose: func(ctx context.Context) bool {
			app.saveWindowState(ctx)
			app.cleanupOnShutdown()
			return false
		},
		Bind: []interface{}{
			app,
		},
		Menu: appMenu,
		// Custom titlebar chrome — closest Wails equivalent to Tauri's
		// titleBarStyle:"Overlay" + hiddenTitle (tauri-plugin-decorum in the
		// Rust app). No direct equivalent for its window-vibrancy blur yet.
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
