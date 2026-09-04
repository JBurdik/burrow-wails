package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ExtensionManifest is the deliberately small v1 extension contract. Extensions
// are separate programs: Burrow reads their manifest but never loads third-party
// code into its own process.
type ExtensionManifest struct {
	APIVersion  int                `json:"apiVersion"`
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Permissions []string           `json:"permissions,omitempty"`
	Commands    []ExtensionCommand `json:"commands,omitempty"`
	Surfaces    []ExtensionSurface `json:"surfaces,omitempty"`
	Settings    []ExtensionSetting `json:"settings,omitempty"`
}

type ExtensionCommand struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// ExtensionSurface is a host-rendered contribution. v1 intentionally supports
// only workspace-pulse, keeping UI native, themed, and keyboard-consistent.
type ExtensionSurface struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

// ExtensionSetting describes a host-rendered configuration field. The host owns
// input rendering and storage, so extensions never inject settings UI.
type ExtensionSetting struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
}

// ExtensionInfo is safe to render in Settings. Path is intentionally omitted
// from the manifest and supplied by Burrow, so a manifest cannot point outside
// the extensions directory.
type ExtensionInfo struct {
	ExtensionManifest
	Dir     string `json:"dir"`
	Enabled bool   `json:"enabled"`
	Error   string `json:"error,omitempty"`
}

var extensionIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func extensionsDir() (string, error) {
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "extensions"), nil
}

func extensionDir(id string) (string, error) {
	if !extensionIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid extension id %q", id)
	}
	root, err := extensionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

func readExtensionManifest(dir string) (ExtensionManifest, error) {
	var manifest ExtensionManifest
	b, err := os.ReadFile(filepath.Join(dir, "extension.json"))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid extension.json: %w", err)
	}
	if manifest.APIVersion != 1 {
		return manifest, fmt.Errorf("unsupported apiVersion %d (expected 1)", manifest.APIVersion)
	}
	if !extensionIDRe.MatchString(manifest.ID) || manifest.Name == "" || manifest.Version == "" {
		return manifest, errors.New("id, name, and version are required")
	}
	for _, command := range manifest.Commands {
		if command.ID == "" || command.Title == "" || command.Command == "" || strings.Contains(command.Command, "/") || strings.Contains(command.Command, `\\`) {
			return manifest, fmt.Errorf("command %q must have id, title, and a PATH executable", command.ID)
		}
	}
	for _, surface := range manifest.Surfaces {
		if surface.ID == "" || surface.Title == "" || surface.Kind != "workspace-pulse" {
			return manifest, fmt.Errorf("surface %q must have id, title, and supported kind workspace-pulse", surface.ID)
		}
	}
	for _, setting := range manifest.Settings {
		if !extensionIDRe.MatchString(setting.ID) || setting.Title == "" || setting.Type != "text" {
			return manifest, fmt.Errorf("setting %q must have a lowercase id, title, and type text", setting.ID)
		}
	}
	return manifest, nil
}

func (a *App) ExtensionsDirectory() (string, error) {
	dir, err := extensionsDir()
	if err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o755)
}

func (a *App) ListExtensions() ([]ExtensionInfo, error) {
	dir, err := a.ExtensionsDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]ExtensionInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !extensionIDRe.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		manifest, manifestErr := readExtensionManifest(path)
		info := ExtensionInfo{ExtensionManifest: manifest, Dir: entry.Name(), Enabled: true}
		if manifestErr != nil {
			info.Error = manifestErr.Error()
		}
		if _, err := os.Stat(filepath.Join(path, ".disabled")); err == nil {
			info.Enabled = false
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a *App) SetExtensionEnabled(id string, enabled bool) error {
	dir, err := extensionDir(id)
	if err != nil {
		return err
	}
	if _, err := readExtensionManifest(dir); err != nil {
		return err
	}
	marker := filepath.Join(dir, ".disabled")
	if enabled {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(marker, []byte("disabled by user\n"), 0o600)
}

const maxExtensionArchiveFiles = 500
const maxExtensionArchiveSize int64 = 50 << 20

// InstallExtension copies a selected extension folder or .zip archive into
// Burrow's managed extension directory. The manifest is validated before the
// installed extension replaces an older version with the same ID.
func (a *App) InstallExtension(source string) error {
	if source == "" {
		return errors.New("choose an extension folder or .zip file")
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "burrow-extension-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	staged := filepath.Join(root, "extension")
	if info.IsDir() {
		if err := copyExtensionDirectory(source, staged); err != nil {
			return err
		}
	} else if strings.EqualFold(filepath.Ext(source), ".zip") {
		if err := extractExtensionArchive(source, staged); err != nil {
			return err
		}
	} else {
		return errors.New("extension source must be a folder or .zip archive")
	}

	manifest, err := readExtensionManifest(staged)
	if err != nil {
		return err
	}
	target, err := extensionDir(manifest.ID)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	// Rename makes the complete, validated extension appear at once. An existing
	// extension with the same manifest ID is replaced only after staging passed.
	backup := target + ".install-backup"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("prepare extension update: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		return fmt.Errorf("install extension: %w", err)
	}
	return os.RemoveAll(backup)
}

func copyExtensionDirectory(source, target string) error {
	if _, err := os.Stat(filepath.Join(source, "extension.json")); err != nil {
		return errors.New("selected folder must contain extension.json")
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return fmt.Errorf("extension cannot contain symlink %q", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("extension contains unsupported file %q", path)
		}
		return copyExtensionFile(path, destination)
	})
}

func extractExtensionArchive(source, target string) error {
	archive, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) > maxExtensionArchiveFiles {
		return fmt.Errorf("archive contains more than %d files", maxExtensionArchiveFiles)
	}

	prefix := ""
	for _, file := range archive.File {
		name := filepath.ToSlash(file.Name)
		if name == "extension.json" {
			if prefix != "" {
				return errors.New("archive contains more than one extension.json")
			}
			prefix = "."
		} else if strings.Count(strings.TrimSuffix(name, "/"), "/") == 1 && strings.HasSuffix(name, "/extension.json") {
			candidate := strings.TrimSuffix(name, "/extension.json")
			if prefix != "" {
				return errors.New("archive contains more than one extension.json")
			}
			prefix = candidate
		}
	}
	if prefix == "" {
		return errors.New("archive must contain extension.json at its root or in one top-level folder")
	}

	var total int64
	for _, file := range archive.File {
		name := filepath.ToSlash(file.Name)
		rel := name
		if prefix != "." {
			if !strings.HasPrefix(name, prefix+"/") {
				continue
			}
			rel = strings.TrimPrefix(name, prefix+"/")
		}
		if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "../") || rel == ".." {
			return fmt.Errorf("unsafe archive path %q", file.Name)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive cannot contain symlink %q", file.Name)
		}
		destination := filepath.Join(target, filepath.FromSlash(rel))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		if !file.Mode().IsRegular() {
			return fmt.Errorf("archive contains unsupported file %q", file.Name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxExtensionArchiveSize {
			return fmt.Errorf("archive expands beyond %d MiB", maxExtensionArchiveSize>>20)
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		err = writeExtensionFile(destination, reader)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func copyExtensionFile(source, target string) error {
	reader, err := os.Open(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	return writeExtensionFile(target, reader)
}

func writeExtensionFile(target string, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	writer, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (a *App) RunExtensionCommand(extensionID, commandID, cwd string) (string, error) {
	dir, err := extensionDir(extensionID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, ".disabled")); err == nil {
		return "", errors.New("extension is disabled")
	}
	manifest, err := readExtensionManifest(dir)
	if err != nil {
		return "", err
	}
	var selected *ExtensionCommand
	for i := range manifest.Commands {
		if manifest.Commands[i].ID == commandID {
			selected = &manifest.Commands[i]
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("unknown command %q", commandID)
	}
	bridge, err := startExtensionBridge(a, manifest, cwd)
	if err != nil {
		return "", fmt.Errorf("start extension bridge: %w", err)
	}
	defer bridge.close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, selected.Command, selected.Args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BURROW_EXTENSION_ID="+manifest.ID, "BURROW_EXTENSION_DIR="+dir, "BURROW_EXTENSION_CWD="+cwd, "BURROW_EXTENSION_BRIDGE_URL="+bridge.url, "BURROW_EXTENSION_BRIDGE_TOKEN="+bridge.token)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), errors.New("extension command timed out after 15 seconds")
	}
	if err != nil {
		return string(out), fmt.Errorf("extension command failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
