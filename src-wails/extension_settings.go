package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func extensionSettingsPath(extensionID string) (string, error) {
	if !extensionIDRe.MatchString(extensionID) {
		return "", fmt.Errorf("invalid extension id %q", extensionID)
	}
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "extension-settings", extensionID+".json"), nil
}

func readExtensionSettings(extensionID string) (map[string]string, error) {
	path, err := extensionSettingsPath(extensionID)
	if err != nil {
		return nil, err
	}
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := json.Unmarshal(contents, &values); err != nil {
		return nil, fmt.Errorf("read extension settings: %w", err)
	}
	return values, nil
}

func writeExtensionSettings(extensionID string, values map[string]string) error {
	path, err := extensionSettingsPath(extensionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o600)
}

// GetExtensionSettings returns only fields declared by the manifest.
func (a *App) GetExtensionSettings(extensionID string) (map[string]string, error) {
	dir, err := extensionDir(extensionID)
	if err != nil {
		return nil, err
	}
	manifest, err := readExtensionManifest(dir)
	if err != nil {
		return nil, err
	}
	stored, err := readExtensionSettings(extensionID)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(manifest.Settings))
	for _, setting := range manifest.Settings {
		values[setting.ID] = stored[setting.ID]
	}
	return values, nil
}

// SaveExtensionSettings only persists manifest-declared text fields and refuses
// unknown keys, keeping extensions from silently accumulating arbitrary config.
func (a *App) SaveExtensionSettings(extensionID string, values map[string]string) error {
	dir, err := extensionDir(extensionID)
	if err != nil {
		return err
	}
	manifest, err := readExtensionManifest(dir)
	if err != nil {
		return err
	}
	allowed := map[string]ExtensionSetting{}
	for _, setting := range manifest.Settings {
		allowed[setting.ID] = setting
	}
	for key, value := range values {
		setting, ok := allowed[key]
		if !ok {
			return fmt.Errorf("unknown setting %q", key)
		}
		if setting.Required && value == "" {
			return fmt.Errorf("%s is required", setting.Title)
		}
	}
	for _, setting := range manifest.Settings {
		if setting.Required && values[setting.ID] == "" {
			return fmt.Errorf("%s is required", setting.Title)
		}
	}
	return writeExtensionSettings(extensionID, values)
}
