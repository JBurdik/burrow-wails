package main

import (
	"sync"
	"time"
)

// Which Burrow windows have focus, as the windows themselves report it
// (src/lib/focusReport.ts). On dev/beta the windows live in another process
// than the core that decides tray toasts / banners, so the core cannot ask
// AppKit. A focused window re-reports every 20 s; a report older than
// uiFocusTTL is ignored, so a window that died while focused can't silence
// notifications for good.

const uiFocusTTL = 45 * time.Second

var uiFocus = struct {
	sync.Mutex
	at   map[string]time.Time // window → last "focused" report; absent = blurred
	seen bool
}{at: map[string]time.Time{}}

// UiFocus records one window's focus state.
func (a *App) UiFocus(window string, focused bool) {
	uiFocus.Lock()
	defer uiFocus.Unlock()
	uiFocus.seen = true
	if focused {
		uiFocus.at[window] = time.Now()
	} else {
		delete(uiFocus.at, window)
	}
}

// reportedFocus: any window focused lately; known = some window ever reported.
func reportedFocus(now time.Time) (focused, known bool) {
	uiFocus.Lock()
	defer uiFocus.Unlock()
	for _, t := range uiFocus.at {
		if now.Sub(t) < uiFocusTTL {
			return true, true
		}
	}
	return false, uiFocus.seen
}
