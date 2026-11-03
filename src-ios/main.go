// Native iOS shell for Burrow: the phone bundle (mobile.html, built from the
// repo's src/ with VITE_TARGET=mobile) in a Wails WebView. No backend here —
// PTYs, git and agents can't run on iOS; the app pairs to a Mac over the
// tailnet exactly like the PWA (/v2/pair → /v2/ws-ticket → /v2/ws).
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:   "Burrow",
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Burrow",
		BackgroundColour: application.NewRGB(13, 13, 13),
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
