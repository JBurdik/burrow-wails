//go:build darwin && cgo

package privacy

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>

static int axTrusted(int prompt) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *vals[] = { prompt ? kCFBooleanTrue : kCFBooleanFalse };
	CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	int ok = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return ok;
}
*/
import "C"

func state(ok bool) string {
	if ok {
		return "granted"
	}
	return "denied"
}

// ScreenRecording: what `screencapture` run by an agent needs. Never prompts.
func ScreenRecording() string { return state(bool(C.CGPreflightScreenCaptureAccess())) }

// RequestScreenRecording shows macOS's one-time prompt and adds the process
// to the Screen Recording list (later calls do nothing).
func RequestScreenRecording() string { return state(bool(C.CGRequestScreenCaptureAccess())) }

// Accessibility: what UI scripting / computer-use tools need. Never prompts.
func Accessibility() string { return state(C.axTrusted(0) != 0) }

// RequestAccessibility adds the process to the Accessibility list and shows
// macOS's prompt that points there.
func RequestAccessibility() string { return state(C.axTrusted(1) != 0) }
