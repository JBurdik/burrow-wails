//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdio.h>
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

// "#rrggbb" → NSColor; fallback when empty / unparsable.
static NSColor *hexColor(const char *hex, NSColor *fallback) {
	unsigned int r, g, b;
	if (hex == NULL || sscanf(hex, "#%02x%02x%02x", &r, &g, &b) != 3) return fallback;
	return [NSColor colorWithSRGBRed:r / 255.0 green:g / 255.0 blue:b / 255.0 alpha:1];
}

// The panel wears the app's current theme (colors from the frontend's CSS
// variables), so it reads as part of Burrow rather than a stock dialog.
static void showGrantPanel(const char *cpath, const char *ctitle, const char *cbody,
		const char *cbg, const char *cfg, const char *cmuted, const char *cborder, int dark) {
	NSString *path = [NSString stringWithUTF8String:cpath];
	NSString *title = [NSString stringWithUTF8String:ctitle];
	NSString *body = [NSString stringWithUTF8String:cbody];
	NSColor *bg = hexColor(cbg, NSColor.windowBackgroundColor);
	NSColor *fg = hexColor(cfg, NSColor.labelColor);
	NSColor *muted = hexColor(cmuted, NSColor.secondaryLabelColor);
	NSColor *border = hexColor(cborder, NSColor.separatorColor);
	dispatch_async(dispatch_get_main_queue(), ^{
		if (grantPanel != nil) [grantPanel close];
		const CGFloat W = 340, H = 300;
		NSPanel *p = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, W, H)
			styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskFullSizeContentView | NSWindowStyleMaskNonactivatingPanel
			backing:NSBackingStoreBuffered defer:NO];
		p.titleVisibility = NSWindowTitleHidden;
		p.titlebarAppearsTransparent = YES;
		p.appearance = [NSAppearance appearanceNamed:dark ? NSAppearanceNameDarkAqua : NSAppearanceNameAqua];
		p.backgroundColor = bg;
		p.movableByWindowBackground = YES;
		p.floatingPanel = YES;
		p.level = NSFloatingWindowLevel;
		p.hidesOnDeactivate = NO;
		p.releasedWhenClosed = NO;
		p.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
		NSView *v = p.contentView;

		NSTextField *heading = [NSTextField labelWithString:title];
		heading.frame = NSMakeRect(20, H - 54, W - 40, 20);
		heading.alignment = NSTextAlignmentCenter;
		heading.font = [NSFont systemFontOfSize:13 weight:NSFontWeightSemibold];
		heading.textColor = fg;
		[v addSubview:heading];

		// The drop well: a bordered tile around the icon, like the app's rows.
		NSView *tile = [[NSView alloc] initWithFrame:NSMakeRect((W - 156) / 2, H - 216, 156, 148)];
		tile.wantsLayer = YES;
		tile.layer.cornerRadius = 10;
		tile.layer.borderWidth = 1;
		tile.layer.borderColor = border.CGColor;
		tile.layer.backgroundColor = [fg colorWithAlphaComponent:0.04].CGColor;
		[v addSubview:tile];

		BurrowGrantIcon *icon = [[BurrowGrantIcon alloc] initWithFrame:NSMakeRect(22, 26, 112, 112)];
		icon.path = path;
		icon.image = [[NSWorkspace sharedWorkspace] iconForFile:path];
		icon.imageScaling = NSImageScaleProportionallyUpOrDown;
		[tile addSubview:icon];

		NSTextField *name = [NSTextField labelWithString:path.lastPathComponent.stringByDeletingPathExtension];
		name.frame = NSMakeRect(4, 6, 148, 18);
		name.alignment = NSTextAlignmentCenter;
		name.font = [NSFont systemFontOfSize:12 weight:NSFontWeightMedium];
		name.textColor = fg;
		name.lineBreakMode = NSLineBreakByTruncatingMiddle;
		[tile addSubview:name];

		NSTextField *label = [NSTextField wrappingLabelWithString:body];
		label.frame = NSMakeRect(24, 18, W - 48, 48);
		label.alignment = NSTextAlignmentCenter;
		label.font = [NSFont systemFontOfSize:12];
		label.textColor = muted;
		[v addSubview:label];

		// Top right, where it does not cover the System Settings list.
		NSRect f = NSScreen.mainScreen.visibleFrame;
		[p setFrameTopLeftPoint:NSMakePoint(NSMaxX(f) - W - 20, NSMaxY(f) - 20)];
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

// GrantTheme is the app's current look ("#rrggbb", "" = system default).
type GrantTheme struct {
	Bg     string `json:"bg"`
	Fg     string `json:"fg"`
	Muted  string `json:"muted"`
	Border string `json:"border"`
	Dark   bool   `json:"dark"`
}

// showGrantHelper opens System Settings at pane and floats the icon of path
// beside it to drag into the list (the Rex onboarding pattern).
func showGrantHelper(path, pane, title, body string, t GrantTheme) {
	if pane != "" {
		_ = exec.Command("/usr/bin/open", pane).Start()
	}
	strs := []*C.char{C.CString(path), C.CString(title), C.CString(body), C.CString(t.Bg), C.CString(t.Fg), C.CString(t.Muted), C.CString(t.Border)}
	defer func() {
		for _, c := range strs {
			C.free(unsafe.Pointer(c))
		}
	}()
	dark := C.int(0)
	if t.Dark {
		dark = 1
	}
	C.showGrantPanel(strs[0], strs[1], strs[2], strs[3], strs[4], strs[5], strs[6], dark)
}

func closeGrantHelper() { C.closeGrantPanel() }
