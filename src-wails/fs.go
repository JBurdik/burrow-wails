package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
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

// OpenPathIn opens path in Finder ("finder"), the default editor ("editor"),
// or Terminal.app ("terminal").
func (a *App) OpenPathIn(path, target string) error {
	switch target {
	case "editor":
		return exec.Command("open", "-t", path).Start()
	case "terminal":
		return exec.Command("open", "-a", "Terminal", path).Start()
	default:
		return exec.Command("open", "-R", path).Start()
	}
}

func (a *App) GetAppVersion() string {
	return appVersion
}

// SetSleepInhibit toggles macOS sleep prevention by starting/stopping a
// background `caffeinate` process, replacing the Rust IOKit binding.
var caffeinateCmd *exec.Cmd

func (a *App) SetSleepInhibit(active bool) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if active {
		if caffeinateCmd != nil {
			return nil
		}
		caffeinateCmd = exec.Command("caffeinate", "-i")
		return caffeinateCmd.Start()
	}
	if caffeinateCmd != nil && caffeinateCmd.Process != nil {
		_ = caffeinateCmd.Process.Kill()
	}
	caffeinateCmd = nil
	return nil
}
