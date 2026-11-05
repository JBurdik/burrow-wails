package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Hold-to-quit (uiPrefs.holdToQuit, default on): ⌘Q quits only when held.
// The ⌘Q menu item fires once per key auto-repeat, so a hold is a run of
// presses no further apart than quitHoldGap. A run that lasts quitHoldDuration
// arms the quit, which then happens once the presses stop — quitting while
// the key is still down would hand the trailing repeats (another ⌘Q) to
// whichever app comes forward next. A run that stops short just cancels.
//
// ponytail: release is inferred from repeats stopping (≤ quitHoldGap late),
// and a key-repeat initial delay above the gap (macOS's slowest settings)
// never builds a run — the tray's Quit / ⌥⌘Q still quit at once.
const (
	quitHoldDuration = time.Second
	quitHoldGap      = 700 * time.Millisecond
)

type quitHold struct {
	mu          sync.Mutex
	since, last time.Time // zero since = no run
	armed       bool
	emit        func(state string) // "down" | "quitting" | "up"
	quit        func()
	after       func(time.Duration, func()) // time.AfterFunc; tests stub it
}

func (h *quitHold) press(now time.Time) {
	h.mu.Lock()
	if h.since.IsZero() || now.Sub(h.last) > quitHoldGap {
		h.since, h.armed = now, false
		h.emit("down")
	}
	h.last = now
	if !h.armed && now.Sub(h.since) >= quitHoldDuration {
		h.armed = true
		h.emit("quitting")
	}
	h.mu.Unlock()
	h.after(quitHoldGap, func() { h.settle(time.Now()) })
}

// settle ends a run once no press came for quitHoldGap; a timer from an
// earlier press of a still-running run is a no-op.
func (h *quitHold) settle(now time.Time) {
	h.mu.Lock()
	if h.since.IsZero() || now.Sub(h.last) < quitHoldGap {
		h.mu.Unlock()
		return
	}
	armed := h.armed
	h.since, h.armed = time.Time{}, false
	h.mu.Unlock()
	if armed {
		h.quit()
	} else {
		h.emit("up")
	}
}

// holdToQuitPref reads uiPrefs.holdToQuit fresh. Absent or unreadable = on:
// the safe side of a guess is not quitting.
func holdToQuitPref() bool {
	path, err := configFilePath()
	if err != nil {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var cfg struct {
		UI struct {
			Hold *bool `json:"holdToQuit"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.UI.Hold == nil {
		return true
	}
	return *cfg.UI.Hold
}
