package main

import (
	"os"
	"os/exec"
	"strings"

	"burrow/internal/daemonproto"
	"burrow/internal/privacy"
)

// The permissions step (onboarding + Settings, plan 009, modelled on Rex):
// each process reports what macOS grants IT, since an app cannot read another
// process's permissions. Reading never prompts.

// PrivacyStatus is what the permissions UI shows.
type PrivacyStatus struct {
	// Notifications: granted · denied · not_determined · webview (in-process:
	// the webview posts them) · unknown.
	Notifications string `json:"notifications"`
	BundleID      string `json:"bundle_id"`
	// Chats run under the core; Full Disk Access is granted to its app bundle.
	ChatsFDA  string `json:"chats_fda"`
	ChatsPath string `json:"chats_path"`
	// Terminals run under burrow-daemon, its own responsible process.
	TerminalsFDA  string `json:"terminals_fda"`
	TerminalsPath string `json:"terminals_path"`
	// Screen Recording / Accessibility of the core, i.e. for chats' tools
	// (screencapture, UI scripting). ponytail: terminals (burrow-daemon) are not
	// read; add a daemon "privacy" field when a tab agent needs them.
	ScreenRecording string `json:"screen_recording"`
	Accessibility   string `json:"accessibility"`
}

// nativeNotifyStatus / nativeNotifyRequest are set by the core role, which
// owns the notifications service; nil in-process.
var (
	nativeNotifyStatus  func() string
	nativeNotifyRequest func() string
)

func (a *App) PrivacyStatus() PrivacyStatus {
	st := PrivacyStatus{Notifications: "webview", ChatsFDA: privacy.FullDiskAccess(), TerminalsFDA: "unknown",
		ScreenRecording: privacy.ScreenRecording(), Accessibility: privacy.Accessibility()}
	if nativeNotifyStatus != nil {
		st.Notifications = nativeNotifyStatus()
	}
	if exe, err := os.Executable(); err == nil {
		st.ChatsPath = exe
		if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
			st.ChatsPath = exe[:i+len(".app")]
			if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", st.ChatsPath+"/Contents/Info.plist").Output(); err == nil {
				st.BundleID = strings.TrimSpace(string(out))
			}
		}
	}
	if a.daemon != nil {
		// An old daemon answers "unknown request kind": terminals stay unknown
		// until it is restarted onto this build.
		if resp, err := a.daemon.call(daemonproto.Request{Kind: "privacy"}); err == nil && resp != nil {
			st.TerminalsFDA, st.TerminalsPath = resp.FDA, resp.ExePath
		}
	}
	return st
}

// RequestNotifications asks macOS for notification permission (the system
// dialog, first time only) and returns the resulting state.
func (a *App) RequestNotifications() string {
	if nativeNotifyRequest == nil {
		return "webview"
	}
	return nativeNotifyRequest()
}

// RequestPrivacy shows macOS's prompt for "screen_recording" / "accessibility",
// which also puts the core into that System Settings list to switch on.
func (a *App) RequestPrivacy(kind string) string {
	switch kind {
	case "screen_recording":
		return privacy.RequestScreenRecording()
	case "accessibility":
		return privacy.RequestAccessibility()
	}
	return "unknown"
}
