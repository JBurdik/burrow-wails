package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// environmentFile is the on-disk shape of <app-data>/environment.json.
type environmentFile struct {
	EnvironmentID string `json:"environmentId"`
}

// environmentID returns the stable identity of this Burrow install, creating
// it on first call. Every client-side record (known environments, endpoint
// preferences, seen-at receipts) is keyed by it, so it must survive changes of
// IP, hostname and tailnet — which is why it is a stored random id rather than
// anything derived from the machine.
func environmentID(dir string) (string, error) {
	path := filepath.Join(dir, "environment.json")
	if b, err := os.ReadFile(path); err == nil {
		var f environmentFile
		// A corrupt or truncated file regenerates rather than failing: an
		// unreadable id must not be able to keep the app from starting.
		if json.Unmarshal(b, &f) == nil && f.EnvironmentID != "" {
			return f.EnvironmentID, nil
		}
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)

	b, err := json.Marshal(environmentFile{EnvironmentID: id})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// EnvironmentID is the Wails binding. The value is resolved once at startup.
func (a *App) EnvironmentID() string { return a.environmentID }
