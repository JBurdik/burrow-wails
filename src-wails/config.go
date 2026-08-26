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
	return os.WriteFile(path, []byte(content), 0o644)
}
