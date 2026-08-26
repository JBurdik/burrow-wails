package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

// The `burrow` CLI and `tmux` shim are unmodified shell scripts, ported
// as-is from src-tauri/bin/{burrow,tmux} — they only talk to the app over
// the file-based request-dir transport and the hook HTTP server, both of
// which are host-language-agnostic. See ensure_burrow_bin in the Rust
// backend for the equivalent write-out-to-PATH mechanism this replaces.
//
//go:embed bin/burrow
var burrowScript []byte

//go:embed bin/tmux
var tmuxShim []byte

// ensureBurrowBin writes the embedded scripts to <appDataDir>/bin/ and
// returns that directory, to be prepended to a spawned PTY's PATH.
func ensureBurrowBin(appDataDir string) (string, error) {
	binDir := filepath.Join(appDataDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(binDir, "burrow"), burrowScript, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), tmuxShim, 0o755); err != nil {
		return "", err
	}
	return binDir, nil
}
