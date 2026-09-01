package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Window geometry persistence: restored on DOM-ready, saved on close.
// Own file (window.json) rather than config.json — config.json is written by
// the frontend wholesale, so a Go-side write would race it.

type windowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

func windowStatePath() (string, error) {
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "window.json"), nil
}

func (a *App) restoreWindowState(ctx context.Context) {
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
		wruntime.WindowSetSize(ctx, st.Width, st.Height)
	}
	// ponytail: sanity bound only — the Wails Screen struct exposes no screen
	// origin, so a real "is this rect on a connected display" clamp isn't
	// possible here. macOS keeps the titlebar reachable in practice; if a
	// detached-monitor case bites, clamp against ScreenGetAll once origins land.
	if abs(st.X) < 20000 && abs(st.Y) < 20000 && (st.X != 0 || st.Y != 0) {
		wruntime.WindowSetPosition(ctx, st.X, st.Y)
	}
	if st.Maximised {
		wruntime.WindowMaximise(ctx)
	}
}

func (a *App) saveWindowState(ctx context.Context) {
	path, err := windowStatePath()
	if err != nil {
		return
	}
	st := windowState{Maximised: wruntime.WindowIsMaximised(ctx)}
	st.X, st.Y = wruntime.WindowGetPosition(ctx)
	st.Width, st.Height = wruntime.WindowGetSize(ctx)
	abnormal := st.Maximised || wruntime.WindowIsFullscreen(ctx) || wruntime.WindowIsMinimised(ctx)
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

// mergeWindowState keeps the last *normal* geometry when the window is
// maximised/fullscreen/minimised (those report the screen rect, not the size
// the user wants back on unmaximise).
func mergeWindowState(prev, cur windowState, abnormal bool) windowState {
	if abnormal && prev.Width >= 400 && prev.Height >= 300 {
		cur.X, cur.Y, cur.Width, cur.Height = prev.X, prev.Y, prev.Width, prev.Height
	}
	return cur
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
