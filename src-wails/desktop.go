//go:build !headless

package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
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
