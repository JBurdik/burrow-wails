package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Self-update, replacing tauri-plugin-updater. Same shape as before: a
// latest.json manifest on GitHub Releases (published by the justfile's
// `release` recipe), a .app.zip artifact, download → verify → swap →
// relaunch.
//
// Verification differs from Tauri's: instead of a separate minisign keypair
// we lean on the Apple Developer ID signature the release build already
// carries. Two mandatory gates, both hard-fail:
//  1. sha256 of the download must match the digest in the HTTPS-fetched manifest
//  2. the extracted bundle must be codesigned by our team (updateTeamID)
//
// That means one less secret to manage than the Tauri flow, and gate 2 is
// exactly what Gatekeeper checks anyway.

const (
	updateRepo     = "JBurdik/burrow-wails"
	updateTeamID   = "9QY36KZ8JP" // Developer ID Application: Jakub Gál
	updatePlatform = "darwin-aarch64"
)

var updateManifestURL = "https://github.com/" + updateRepo + "/releases/latest/download/latest.json"

// UpdateInfo is what the frontend's updater shim needs (see
// src/lib/wailsCompat/updater.ts).
type UpdateInfo struct {
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	CurrentVersion string `json:"current_version"`
	Notes          string `json:"notes"`
	URL            string `json:"url"`
	SHA256         string `json:"sha256"`
}

type latestJSON struct {
	Version   string `json:"version"`
	Notes     string `json:"notes"`
	Platforms map[string]struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"platforms"`
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
	if p, ok := manifest.Platforms[updatePlatform]; ok {
		info.URL, info.SHA256 = p.URL, p.SHA256
	}
	info.Available = info.URL != "" && versionGreater(manifest.Version, appVersion)
	return info, nil
}

// InstallUpdate downloads the artifact, verifies it, swaps the running .app
// bundle for the new one and leaves the app ready to relaunch. Progress is
// reported on the "update:progress" event as {received, total} byte counts.
func (a *App) InstallUpdate(url, wantSum string) error {
	appBundle, err := runningAppBundle()
	if err != nil {
		return err
	}
	if wantSum == "" {
		return fmt.Errorf("manifest has no sha256 for %s — refusing to install unverified", updatePlatform)
	}

	staging, err := os.MkdirTemp(filepath.Dir(appBundle), ".burrow-update-*")
	if err != nil {
		// Fall back to the system temp dir; the swap then needs a copy rather
		// than a rename, so keep it on the same volume when we can.
		staging, err = os.MkdirTemp("", "burrow-update-*")
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(staging)

	archive := filepath.Join(staging, "update.zip")
	gotSum, err := a.downloadTo(url, archive)
	if err != nil {
		return err
	}
	if !strings.EqualFold(gotSum, wantSum) {
		return fmt.Errorf("checksum mismatch: manifest says %s, download is %s — refusing to install", wantSum, gotSum)
	}

	// Use ditto for the matching macOS ZIP format. Generic tar archives omit
	// the extended attributes and resource metadata a signed .app needs, which
	// turns a valid Developer ID bundle into an unsigned one after extraction.
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", archive, staging).CombinedOutput(); err != nil {
		return fmt.Errorf("extract failed: %v: %s", err, out)
	}

	newBundle, err := findAppBundle(staging)
	if err != nil {
		return err
	}
	if err := verifyBundleSignature(newBundle); err != nil {
		return err
	}

	backup := appBundle + ".old"
	os.RemoveAll(backup)
	if err := os.Rename(appBundle, backup); err != nil {
		return fmt.Errorf("could not move the current app aside (is it in a read-only location?): %w", err)
	}
	if err := os.Rename(newBundle, appBundle); err != nil {
		os.Rename(backup, appBundle) // best-effort rollback
		return fmt.Errorf("could not install the new app: %w", err)
	}
	os.RemoveAll(backup)
	return nil
}

// RelaunchApp starts a fresh copy of the (possibly just-updated) bundle and
// quits this one. Backs "@tauri-apps/plugin-process"'s relaunch().
func (a *App) RelaunchApp() error {
	bundle, err := runningAppBundle()
	if err != nil {
		return err
	}
	// -n forces a new instance so `open` doesn't just focus the dying one.
	if err := exec.Command("/usr/bin/open", "-n", bundle).Start(); err != nil {
		return err
	}
	wruntime.Quit(a.ctx)
	return nil
}

// downloadTo streams url into path, emitting progress events, and returns the
// hex sha256 of what it wrote.
func (a *App) downloadTo(url, path string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	total := resp.ContentLength
	var received int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", werr
			}
			hasher.Write(buf[:n])
			received += int64(n)
			wruntime.EventsEmit(a.ctx, "update:progress", map[string]int64{"received": received, "total": total})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// runningAppBundle resolves .../Burrow.app from the running executable at
// .../Burrow.app/Contents/MacOS/<exe>.
func runningAppBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	bundle := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	if !strings.HasSuffix(bundle, ".app") {
		return "", fmt.Errorf("not running from a .app bundle (%s) — self-update only works on an installed app", bundle)
	}
	// Gatekeeper runs a still-quarantined app from a read-only randomised
	// path instead of where it actually lives, so the swap below would fail
	// with a confusing rename error. Dragging the app in Finder clears the
	// quarantine flag; a scripted copy does not.
	if strings.Contains(bundle, "/AppTranslocation/") {
		return "", fmt.Errorf("Burrow is running from a temporary read-only copy (macOS App Translocation) and cannot update itself.\n\nMove Burrow.app to your Applications folder in Finder and open it from there, or run:\n  xattr -dr com.apple.quarantine \"/Applications/Burrow.app\"")
	}
	return bundle, nil
}

// findAppBundle locates the single top-level .app in an extracted archive.
// Looked up by suffix, not by name, so a locally renamed install still works.
func findAppBundle(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("the downloaded archive contains no .app bundle")
}

// verifyBundleSignature refuses anything not signed by our Developer ID team.
// This is the real trust anchor for the update (HTTPS + the manifest digest
// only prove the bytes are the ones GitHub is serving).
func verifyBundleSignature(bundle string) error {
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--deep", bundle).CombinedOutput(); err != nil {
		return fmt.Errorf("downloaded app failed signature verification: %v: %s", err, out)
	}
	out, err := exec.Command("/usr/bin/codesign", "-dv", bundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not read the downloaded app's signature: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "TeamIdentifier="+updateTeamID) {
		return fmt.Errorf("downloaded app is not signed by team %s — refusing to install", updateTeamID)
	}
	return nil
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
