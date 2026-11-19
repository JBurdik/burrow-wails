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
// handler runs, fix the width — but ONLY in that shape: height taken, width
// off by more than a point (usually the old width; on a display narrower than
// the window, some screen-fitted width instead). Anything else is AppKit's own
// legitimate adjustment (pixel rounding, the vertical screen constraint);
// overriding those made a tiler's every resize jump twice.
static IMP origAXSet;
static void axSetValue(NSWindow *self, SEL cmd, id value, NSString *attr) {
	((void (*)(id, SEL, id, NSString *))origAXSet)(self, cmd, value, attr);
	if (![attr isEqualToString:NSAccessibilitySizeAttribute] || ![value isKindOfClass:[NSValue class]]) return;
	NSSize want = [value sizeValue];
	want.width = MIN(MAX(want.width, self.minSize.width), self.maxSize.width);
	NSRect f = self.frame;
	BOOL widthIgnored = fabs(f.size.width - want.width) > 1;
	BOOL heightTaken = fabs(f.size.height - want.height) <= 1;
	if (!widthIgnored || !heightTaken) return;
	f.size.width = round(want.width);
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
