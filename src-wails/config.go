package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// configMu serialises every config.json write: ConfigSet is a read-modify-write.
var configMu sync.Mutex

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
	configMu.Lock()
	defer configMu.Unlock()
	// Atomic: config.json is one grab-bag of every preference, and a plain
	// truncate-and-write left it corrupt (losing all settings) if the app died
	// mid-save.
	return writeFileAtomic(path, []byte(content))
}

// ConfigSet writes ONE key into config.json and tells every client. It is what
// setConfig uses: several windows (and paired phones) each hold their own copy
// of the config, and the old whole-file WriteConfig let one client's stale
// copy silently revert another's change — the Settings window saving a theme
// that the main window then overwrote on its next unrelated save.
func (a *App) ConfigSet(key string, value json.RawMessage) error {
	if key == "" {
		return errors.New("config key required")
	}
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	path, err := configFilePath()
	if err != nil {
		return err
	}
	configMu.Lock()
	defer configMu.Unlock()
	m := map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			// Refuse rather than replace: overwriting an unreadable file with
			// one key would drop every other setting.
			return fmt.Errorf("config.json is not valid JSON: %w", err)
		}
	}
	m[key] = value
	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, out); err != nil {
		return err
	}
	busEmit("config-changed", map[string]any{"key": key, "value": value})
	return nil
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
