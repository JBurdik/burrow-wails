// burrow-daemon is the standalone PTY-holding process: it keeps sessions
// alive across app restarts, matching src-tauri/src/daemon_main.rs.
package main

import (
	"log"
	"os"
	"path/filepath"

	"burrow/internal/daemonserver"
)

func main() {
	sockPath := os.Getenv("BURROW_DAEMON_SOCK")
	if sockPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("home dir: %v", err)
		}
		dir := filepath.Join(home, "Library", "Application Support", "burrow-wails")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
		sockPath = filepath.Join(dir, "daemon.sock")
	}

	log.Printf("burrow-daemon listening on %s", sockPath)
	if err := daemonserver.New(sockPath).ListenAndServe(); err != nil {
		log.Fatalf("daemon: %v", err)
	}
}
