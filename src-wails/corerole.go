//go:build !headless

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Plan 009: on dev and beta the backend (App) runs as its own process —
// `Burrow --core` — and the window process is only a client of its /v2/ws,
// like the phone. Stable keeps the in-process App until it switches over.

// externalCore: every channel but stable ("") splits the backend out.
func externalCore() bool { return buildChannel != "" }

func coreRoleRequested() bool {
	for _, a := range os.Args[1:] {
		if a == "--core" {
			return true
		}
	}
	return false
}

// runCore is the backend with no window: what main_headless.go does, minus
// forcing remote access on (the desktop's Settings owns that toggle).
func runCore() {
	dataDir, err := appDataDir()
	if err != nil {
		log.Fatalf("core: app data dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("core: %v", err)
	}
	// One core per data dir: a UI and a CLI racing to start one must not get
	// two backends writing the same SQLite and config.json.
	lock, err := os.OpenFile(filepath.Join(dataDir, "core.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Fatalf("core: lock: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Printf("core: another core owns %s, exiting", dataDir)
		return
	}
	app := NewApp()
	app.startup(context.Background())
	log.Printf("core up (pid %d, hook port %d)", os.Getpid(), app.hookPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("core: shutting down")
	app.cleanupOnShutdown()
}

// uiLocalEndpoint redeems control.token for a ticket at the running core.
func uiLocalEndpoint() (LocalEndpointInfo, error) {
	var info LocalEndpointInfo
	dataDir, err := appDataDir()
	if err != nil {
		return info, err
	}
	port, err := os.ReadFile(filepath.Join(dataDir, "hook.port"))
	if err != nil {
		return info, err
	}
	token, err := os.ReadFile(filepath.Join(dataDir, "control.token"))
	if err != nil {
		return info, err
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/v2/local-ticket", strings.TrimSpace(string(port))), nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("local-ticket: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	if info.WSURL == "" {
		return info, fmt.Errorf("local-ticket: core not ready")
	}
	return info, nil
}

var ensureCoreMu sync.Mutex

// ensureCore starts `Burrow --core` unless one answers, then waits for it.
// Serialized: every window asks for a ticket on (re)connect at once.
func ensureCore() (LocalEndpointInfo, error) {
	ensureCoreMu.Lock()
	defer ensureCoreMu.Unlock()
	if info, err := uiLocalEndpoint(); err == nil {
		return info, nil
	}
	dataDir, err := appDataDir()
	if err != nil {
		return LocalEndpointInfo{}, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil { // first run: the launcher writes its plist there
		return LocalEndpointInfo{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return LocalEndpointInfo{}, err
	}
	// Never delete hook.port here: if the core is alive and only slow, the CLI
	// and every hook would lose it. A new core rewrites it.
	log.Printf("ui: starting core")
	if err := spawnDetached("core", []string{exe, "--core"}, os.Environ(), filepath.Join(dataDir, "core.log"), filepath.Join(dataDir, "hook.port")); err != nil {
		return LocalEndpointInfo{}, err
	}
	var last error
	for i := 0; i < 150; i++ { // ≤ 15 s: a cold start migrates SQLite, starts the daemon
		time.Sleep(100 * time.Millisecond)
		info, err := uiLocalEndpoint()
		if err == nil {
			return info, nil
		}
		last = err
	}
	return LocalEndpointInfo{}, fmt.Errorf("core did not come up: %w", last)
}
