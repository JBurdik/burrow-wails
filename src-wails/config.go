package main

import (
	"os"
	"path/filepath"
)

// The app's own JSON config file (agent presets, UI prefs not covered by
// SQLite) — matches read_config/write_config in src-tauri/src/lib.rs.

func configFilePath() (string, error) {
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "config.json"), nil
}

// ConfigFilePath exposes the on-disk config.json path so the UI can offer
// "edit this by hand" (Settings → Keybindings).
func (a *App) ConfigFilePath() (string, error) {
	return configFilePath()
}

func (a *App) ReadConfig() (string, error) {
	path, err := configFilePath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "{}", nil
		}
		return "", err
	}
	return string(b), nil
}

func (a *App) WriteConfig(content string) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	// Atomic: config.json is one grab-bag of every preference, and a plain
	// truncate-and-write left it corrupt (losing all settings) if the app died
	// mid-save.
	return writeFileAtomic(path, []byte(content))
}

// Keybindings live in their own keybindings.json — config.json is a grab-bag
// (chat history alone runs to hundreds of KB), and shortcuts are the one part
// users hand-edit.

func keybindingsFilePath() (string, error) {
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "keybindings.json"), nil
}

func (a *App) KeybindingsFilePath() (string, error) {
	return keybindingsFilePath()
}

func (a *App) ReadKeybindings() (string, error) {
	path, err := keybindingsFilePath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "{}", nil
		}
		return "", err
	}
	return string(b), nil
}

func (a *App) WriteKeybindings(content string) error {
	path, err := keybindingsFilePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
