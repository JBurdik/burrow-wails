// Package privacy reads the macOS privacy permissions of the CALLING process
// without ever making macOS ask the person anything (plan 009 permissions).
// macOS grants them to the process it holds responsible — for Burrow the core
// (chats and their tools) and the PTY daemon (every terminal) — so each one
// reports for itself; an app cannot read another process's grants.
package privacy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// fdaProbes are directories only Full Disk Access opens, and which fail with
// EPERM silently (no prompt) without it — unlike Desktop / Documents, which
// prompt, and app containers, which macOS 14+ guards with "App Data" instead.
// The user TCC.db is not used: newer macOS no longer keeps it in the home.
func fdaProbes() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/Library/Application Support/com.apple.TCC",
		filepath.Join(home, "Library", "Safari"),
		filepath.Join(home, "Library", "Mail"),
		filepath.Join(home, "Library", "Messages"),
	}
}

// FullDiskAccess: "granted" · "denied" · "unknown" (not macOS / no probe exists).
func FullDiskAccess() string {
	denied := false
	for _, p := range fdaProbes() {
		f, err := os.Open(p)
		if err == nil {
			f.Close()
			return "granted"
		}
		if errors.Is(err, fs.ErrPermission) {
			denied = true
		}
	}
	if denied {
		return "denied"
	}
	return "unknown"
}
