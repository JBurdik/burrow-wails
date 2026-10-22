package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Generic file I/O + misc system bindings, matching write_text_file/
// read_text_file/read_file_base64/read_dir_shallow/open_path_in/
// get_app_version/set_sleep_inhibit in src-tauri/src/lib.rs.

func (a *App) WriteTextFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func (a *App) ReadTextFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (a *App) ReadFileBase64(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// CreateDir makes path (and parents), so the in-app directory picker can
// create a project folder without a native dialog.
func (a *App) CreateDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func (a *App) ReadDirShallow(path string) ([]DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, DirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return out, nil
}

// OpenPathIn opens path in Finder ("finder"/"" reveal), the default editor
// ("editor"), Terminal.app ("terminal"), or any installed app bundle path
// as returned by ListOpenTargets (e.g. "/Applications/Zed.app").
func (a *App) OpenPathIn(path, target string) error {
	switch target {
	case "", "finder":
		return exec.Command("open", "-R", path).Start()
	case "editor":
		return exec.Command("open", "-t", path).Start()
	case "terminal":
		return exec.Command("open", "-a", "Terminal", path).Start()
	default:
		if !strings.HasSuffix(target, ".app") {
			return exec.Command("open", "-R", path).Start()
		}
		return exec.Command("open", "-a", target, path).Start()
	}
}

// OpenTarget is one entry in the "Open in…" picker: Finder or an installed app.
type OpenTarget struct {
	ID   string `json:"id"`   // "finder" or an app bundle path, also used as the OpenPathIn target
	Name string `json:"name"` // display label
	Icon string `json:"icon"` // "data:image/png;base64,..." or "" if extraction failed
}

// openTargetCandidates is the curated list of editors/IDEs/terminals we look
// for in /Applications — ponytail: a fixed list + existence check instead of
// a full LaunchServices query, since the goal is "apps this dev actually has".
var openTargetCandidates = []struct {
	Names []string // possible bundle folder names, checked in order
	Label string
}{
	{[]string{"Visual Studio Code.app"}, "VS Code"},
	{[]string{"Cursor.app"}, "Cursor"},
	{[]string{"Windsurf.app"}, "Windsurf"},
	{[]string{"Zed.app"}, "Zed"},
	{[]string{"Sublime Text.app"}, "Sublime Text"},
	{[]string{"WebStorm.app"}, "WebStorm"},
	{[]string{"IntelliJ IDEA.app", "IntelliJ IDEA CE.app"}, "IntelliJ IDEA"},
	{[]string{"PyCharm.app", "PyCharm CE.app"}, "PyCharm"},
	{[]string{"Xcode.app"}, "Xcode"},
	{[]string{"Android Studio.app"}, "Android Studio"},
	{[]string{"Warp.app"}, "Warp"},
	{[]string{"iTerm.app"}, "iTerm"},
}

func appSearchDirs() []string {
	return []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")}
}

func findInstalledApp(names []string) string {
	for _, dir := range appSearchDirs() {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

var (
	iconCacheMu sync.Mutex
	iconCache   = map[string]string{}
)

// appIconDataURL extracts an app bundle's icon via `defaults`/`sips` (both
// macOS built-ins, no new dependency) and caches the PNG data-URL in memory
// for the process lifetime, since bundles don't move while Burrow is running.
func appIconDataURL(bundlePath string) string {
	iconCacheMu.Lock()
	if v, ok := iconCache[bundlePath]; ok {
		iconCacheMu.Unlock()
		return v
	}
	iconCacheMu.Unlock()

	dataURL := extractAppIcon(bundlePath)
	iconCacheMu.Lock()
	iconCache[bundlePath] = dataURL
	iconCacheMu.Unlock()
	return dataURL
}

func extractAppIcon(bundlePath string) string {
	out, err := exec.Command("defaults", "read", filepath.Join(bundlePath, "Contents", "Info"), "CFBundleIconFile").Output()
	if err != nil {
		return ""
	}
	iconFile := strings.TrimSpace(string(out))
	if iconFile == "" {
		return ""
	}
	if !strings.HasSuffix(iconFile, ".icns") {
		iconFile += ".icns"
	}
	icnsPath := filepath.Join(bundlePath, "Contents", "Resources", iconFile)
	if _, err := os.Stat(icnsPath); err != nil {
		return ""
	}

	tmp, err := os.CreateTemp("", "burrow-icon-*.png")
	if err != nil {
		return ""
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := exec.Command("sips", "-s", "format", "png", icnsPath, "--out", tmpPath, "-Z", "64").Run(); err != nil {
		return ""
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// ListOpenTargets returns Finder plus every installed app from
// openTargetCandidates found on disk, each with an extracted icon.
func (a *App) ListOpenTargets() []OpenTarget {
	targets := []OpenTarget{{ID: "finder", Name: "Finder", Icon: appIconDataURL("/System/Library/CoreServices/Finder.app")}}
	for _, c := range openTargetCandidates {
		bundle := findInstalledApp(c.Names)
		if bundle == "" {
			continue
		}
		targets = append(targets, OpenTarget{ID: bundle, Name: c.Label, Icon: appIconDataURL(bundle)})
	}
	return targets
}

func (a *App) GetAppVersion() string {
	return appVersion
}

// Sleep prevention is one background `caffeinate`, replacing the Rust IOKit
// binding, held for two independent reasons: an agent is busy (the frontend's
// SetSleepInhibit) or remote access is on (setRemoteAwake), so a paired phone
// can still reach the Mac. Remote adds -s, which macOS honours on AC power
// only, so it never drains a battery; a closed lid still sleeps regardless.
var (
	sleepMu       sync.Mutex
	sleepBusy     bool
	sleepRemote   bool
	caffeinateCmd *exec.Cmd
	caffeinateArg string
)

func (a *App) SetSleepInhibit(active bool) error {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	sleepBusy = active
	return applySleepInhibit()
}

func setRemoteAwake(on bool) {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	sleepRemote = on
	_ = applySleepInhibit()
}

// caffeinateFlags is the flag set for the current reasons, "" = none.
func caffeinateFlags(busy, remote bool) string {
	switch {
	case remote:
		return "-is"
	case busy:
		return "-i"
	}
	return ""
}

// applySleepInhibit runs under sleepMu.
func applySleepInhibit() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	want := caffeinateFlags(sleepBusy, sleepRemote)
	if want == caffeinateArg {
		return nil
	}
	if caffeinateCmd != nil && caffeinateCmd.Process != nil {
		_ = caffeinateCmd.Process.Kill()
		_ = caffeinateCmd.Wait()
	}
	caffeinateCmd, caffeinateArg = nil, ""
	if want == "" {
		return nil
	}
	// -w: caffeinate exits with us, so a crash can't leave the Mac awake.
	cmd := exec.Command("caffeinate", want, "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return err
	}
	caffeinateCmd, caffeinateArg = cmd, want
	return nil
}
