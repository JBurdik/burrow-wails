package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local-only events: the native window is the only consumer, so they may call
// desktopEmit (the Wails window) directly. Everything else goes through busEmit, or the
// mobile client silently never sees it — which is exactly how
// emitWorkspacesChanged went missing.
var wailsRuntimeAllowlist = map[string]string{
	"desktop.go":          "defines desktopEmit",
	"main_headless.go":    "defines the no-op desktopEmit",
	"main.go":             "menu items",
	"updater.go":          "update:progress, desktop-only",
	"stubs.go":            "float window snapshots, desktop-only",
	"lsp.go":              "lsp-msg, desktop-only",
	"extension_bridge.go": "extension-task, desktop-only",
}

func TestWailsRuntimeEmitIsConfined(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "desktopEmit(") && !strings.Contains(string(b), ".Event.Emit(") {
			continue
		}
		if _, ok := wailsRuntimeAllowlist[name]; !ok {
			t.Errorf("%s calls desktopEmit directly; use busEmit so the event reaches remote clients too "+
				"(or add it to wailsRuntimeAllowlist with a reason if it is genuinely desktop-only)", name)
		}
	}
}
