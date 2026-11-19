//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

// AppKit's own AXSize handler (-[NSWindow accessibilitySetValue:forAttribute:])
// drops a width *shrink* when the write lands right after an AXPosition that
// moved the window onto another display: it receives {w, h} but calls
// setFrame with the new height and the OLD width (macOS 27, any stock
// NSWindow — TextEdit and a bare test window do it too; Chromium apps use their
// own AX window and don't). Window managers and screen streamers (Sill) do
// exactly position → size → position, so Burrow kept its width. After AppKit's
// handler runs, apply the size it was asked for, keeping the top-left corner
// as AXSize means, clamped to the window's min/max.
static IMP origAXSet;
static void axSetValue(NSWindow *self, SEL cmd, id value, NSString *attr) {
	((void (*)(id, SEL, id, NSString *))origAXSet)(self, cmd, value, attr);
	if (![attr isEqualToString:NSAccessibilitySizeAttribute] || ![value isKindOfClass:[NSValue class]]) return;
	NSSize want = [value sizeValue];
	NSSize lo = self.minSize, hi = self.maxSize;
	want.width = MIN(MAX(want.width, lo.width), hi.width);
	want.height = MIN(MAX(want.height, lo.height), hi.height);
	NSRect f = self.frame;
	if (NSEqualSizes(f.size, want)) return;
	f.origin.y += f.size.height - want.height;
	f.size = want;
	[self setFrame:f display:YES];
}

static void fixAXSize(void *w) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		Class c = [(__bridge NSWindow *)w class]; // Wails' WebviewWindow: every Burrow window
		SEL s = @selector(accessibilitySetValue:forAttribute:);
		Method m = class_getInstanceMethod(c, s);
		origAXSet = method_getImplementation(m);
		// add on the subclass, so NSWindow's own method is never replaced
		if (!class_addMethod(c, s, (IMP)axSetValue, method_getTypeEncoding(m))) {
			method_setImplementation(m, (IMP)axSetValue);
		}
	});
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// fixAXSize makes Burrow's windows take an Accessibility size write that
// follows a move onto another display (see the C comment above).
func fixAXSize(w *application.WebviewWindow) {
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			C.fixAXSize(p)
		}
	})
}
