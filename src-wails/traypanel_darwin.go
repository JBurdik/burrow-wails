//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Wails makes every window movable by its background, and an NSPanel hides
// whenever the app deactivates — both wrong for the tray panel.
static void trayPanelPrepare(void *w) {
	NSWindow *win = (__bridge NSWindow *)w;
	[win setHidesOnDeactivate:NO];
	[win setMovableByWindowBackground:NO];
	[win setMovable:NO];
}

static void trayPanelSetMovable(void *w, bool movable) {
	[(__bridge NSWindow *)w setMovable:movable];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// prepareTrayPanel: stays up when Burrow is not the active app (pinned panel
// = always on top for real), and is not draggable until pinned.
func prepareTrayPanel(w *application.WebviewWindow) {
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			C.trayPanelPrepare(p)
		}
	})
}

// setTrayPanelMovable: only a pinned panel moves (by its header, via
// --wails-draggable); unpinned it is anchored under the tray icon.
func setTrayPanelMovable(w *application.WebviewWindow, movable bool) {
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			C.trayPanelSetMovable(p, C.bool(movable))
		}
	})
}
