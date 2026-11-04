//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

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

// A click on a window that is not key is, for WKWebView, only a focus click:
// the page never sees it. The tray toast is never key (it must not take the
// keys from the user's app), so its webview takes the first click — only in
// panels (the tray panel, the toast); every other window keeps the default.
static IMP origAcceptsFirstMouse;
static BOOL panelAcceptsFirstMouse(id self, SEL cmd, NSEvent *e) {
	if ([[self window] isKindOfClass:[NSPanel class]]) return YES;
	return ((BOOL (*)(id, SEL, NSEvent *))origAcceptsFirstMouse)(self, cmd, e);
}

static void trayToastPrepare(void *w) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		Class c = [WKWebView class];
		SEL s = @selector(acceptsFirstMouse:);
		Method m = class_getInstanceMethod(c, s);
		origAcceptsFirstMouse = method_getImplementation(m);
		// add on WKWebView itself, so an inherited NSView method is never replaced
		if (!class_addMethod(c, s, (IMP)panelAcceptsFirstMouse, method_getTypeEncoding(m))) {
			method_setImplementation(m, (IMP)panelAcceptsFirstMouse);
		}
	});
	trayPanelPrepare(w);
	NSWindow *win = (__bridge NSWindow *)w;
	[win setIgnoresMouseEvents:NO];
	[win setHasShadow:NO]; // the cards draw their own; a window shadow outlines the gaps
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

// prepareTrayToast: like the panel (stays up while Burrow is inactive, never
// dragged), and its first click reaches the page.
func prepareTrayToast(w *application.WebviewWindow) {
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			C.trayToastPrepare(p)
		}
	})
}
