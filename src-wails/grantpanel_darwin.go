//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// An icon you drag (as a file URL) into a System Settings privacy list —
// the Full Disk Access list takes apps and plain executables alike.
@interface BurrowGrantIcon : NSImageView <NSDraggingSource>
@property (copy) NSString *path;
@end

@implementation BurrowGrantIcon
- (NSDragOperation)draggingSession:(NSDraggingSession *)session sourceOperationMaskForDraggingContext:(NSDraggingContext)context {
	return NSDragOperationCopy | NSDragOperationLink | NSDragOperationGeneric;
}
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (BOOL)mouseDownCanMoveWindow { return NO; }
- (void)mouseDown:(NSEvent *)event {
	NSDraggingItem *item = [[NSDraggingItem alloc] initWithPasteboardWriter:[NSURL fileURLWithPath:self.path]];
	[item setDraggingFrame:self.bounds contents:self.image];
	[self beginDraggingSessionWithItems:@[ item ] event:event source:self];
}
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:NSCursor.openHandCursor]; }
@end

static NSPanel *grantPanel;

static void showGrantPanel(const char *cpath, const char *ctitle, const char *cbody) {
	NSString *path = [NSString stringWithUTF8String:cpath];
	NSString *title = [NSString stringWithUTF8String:ctitle];
	NSString *body = [NSString stringWithUTF8String:cbody];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (grantPanel != nil) [grantPanel close];
		NSPanel *p = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 300, 170)
			styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskUtilityWindow | NSWindowStyleMaskNonactivatingPanel
			backing:NSBackingStoreBuffered defer:NO];
		p.title = title;
		p.floatingPanel = YES;
		p.level = NSFloatingWindowLevel;
		p.hidesOnDeactivate = NO;
		p.releasedWhenClosed = NO;
		p.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
		NSView *v = p.contentView;

		BurrowGrantIcon *icon = [[BurrowGrantIcon alloc] initWithFrame:NSMakeRect(118, 82, 64, 64)];
		icon.path = path;
		icon.image = [[NSWorkspace sharedWorkspace] iconForFile:path];
		[v addSubview:icon];

		NSTextField *name = [NSTextField labelWithString:path.lastPathComponent];
		name.frame = NSMakeRect(10, 60, 280, 18);
		name.alignment = NSTextAlignmentCenter;
		name.font = [NSFont boldSystemFontOfSize:12];
		[v addSubview:name];

		NSTextField *label = [NSTextField wrappingLabelWithString:body];
		label.frame = NSMakeRect(16, 10, 268, 44);
		label.alignment = NSTextAlignmentCenter;
		label.font = [NSFont systemFontOfSize:11];
		label.textColor = NSColor.secondaryLabelColor;
		[v addSubview:label];

		// Top right, where it does not cover the System Settings list.
		NSRect f = NSScreen.mainScreen.visibleFrame;
		[p setFrameTopLeftPoint:NSMakePoint(NSMaxX(f) - 320, NSMaxY(f) - 20)];
		[p orderFrontRegardless];
		grantPanel = p;
	});
}

static void closeGrantPanel(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (grantPanel != nil) { [grantPanel close]; grantPanel = nil; }
	});
}
*/
import "C"

import (
	"os/exec"
	"unsafe"
)

// showGrantHelper opens System Settings at pane and floats the icon of path
// beside it to drag into the list (the Rex onboarding pattern).
func showGrantHelper(path, pane, title, body string) {
	if pane != "" {
		_ = exec.Command("/usr/bin/open", pane).Start()
	}
	cp, ct, cb := C.CString(path), C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(cp))
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cb))
	C.showGrantPanel(cp, ct, cb)
}

func closeGrantHelper() { C.closeGrantPanel() }
