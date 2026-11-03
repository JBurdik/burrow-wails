package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// openLogFile opens path for appending, first moving a file over 5 MB to
// path+".1" so the log can't grow without bound.
func openLogFile(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// setupAppLog tees the standard logger into <app-data>/burrow.log: inside a
// .app stdout/stderr go nowhere, so log.Printf left no trace of anything.
func setupAppLog() {
	dir, err := appDataDir()
	if err != nil {
		return
	}
	f, err := openLogFile(filepath.Join(dir, "burrow.log"))
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

// RestartDaemon kills the PTY daemon (every live terminal with it) and starts
// a fresh one. Returns the new daemon's pid.
func (a *App) RestartDaemon() (int, error) {
	if a.daemon == nil {
		return 0, fmt.Errorf("daemon client not initialised")
	}
	return a.daemon.Restart()
}
