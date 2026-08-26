package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// UpdateInfo mirrors what the frontend's updater shim needs (see
// src/lib/wailsCompat/updater.ts) — a real version check against GitHub
// Releases' latest.json (same manifest format docs/plans' release flow
// produces), matching tauri-plugin-updater's endpoint. Download+install
// isn't implemented yet (no Go equivalent of the ed25519-signed
// tauri-plugin-updater artifact flow) — CheckUpdate only reports whether a
// newer version exists.
type UpdateInfo struct {
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	CurrentVersion string `json:"current_version"`
	Notes          string `json:"notes"`
}

const updateManifestURL = "https://github.com/JBurdik/burrow/releases/latest/download/latest.json"

type latestJSON struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

func (a *App) CheckUpdate() (UpdateInfo, error) {
	resp, err := http.Get(updateManifestURL)
	if err != nil {
		return UpdateInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("update manifest: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UpdateInfo{}, err
	}
	var manifest latestJSON
	if err := json.Unmarshal(body, &manifest); err != nil {
		return UpdateInfo{}, err
	}

	info := UpdateInfo{
		Version:        manifest.Version,
		CurrentVersion: appVersion,
		Notes:          manifest.Notes,
	}
	info.Available = versionGreater(manifest.Version, appVersion)
	return info, nil
}

// versionGreater does a numeric-aware "a.b.c" comparison (no pre-release
// suffix support — matches this project's plain semver tags).
func versionGreater(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}
