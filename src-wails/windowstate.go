package main

import (
	"path/filepath"
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
