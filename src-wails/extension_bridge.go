package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// extensionBridge is a short-lived, loopback-only capability API. A new random
// token is created for every command invocation, so an extension never gets a
// reusable application credential.
type extensionBridge struct {
	listener net.Listener
	server   *http.Server
	url      string
	token    string
	app      *App
	manifest ExtensionManifest
	cwd      string
}

var extensionSecretNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

func startExtensionBridge(app *App, manifest ExtensionManifest, cwd string) (*extensionBridge, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = listener.Close()
		return nil, err
	}
	bridge := &extensionBridge{
		listener: listener,
		url:      "http://" + listener.Addr().String(),
		token:    hex.EncodeToString(tokenBytes),
		app:      app,
		manifest: manifest,
		cwd:      cwd,
	}
	bridge.server = &http.Server{Handler: http.HandlerFunc(bridge.handle)}
	go func() { _ = bridge.server.Serve(listener) }()
	return bridge, nil
}

func (b *extensionBridge) close() { _ = b.server.Close() }

func (b *extensionBridge) hasPermission(permission string) bool {
	for _, declared := range b.manifest.Permissions {
		if declared == permission {
			return true
		}
	}
	return false
}

func (b *extensionBridge) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(b.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/v1/workspace":
		b.workspace(w)
	case strings.HasPrefix(r.URL.Path, "/v1/secrets/"):
		b.secrets(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/settings/"):
		b.settings(w, r)
	case r.URL.Path == "/v1/tasks/report":
		b.reportTask(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *extensionBridge) settings(w http.ResponseWriter, r *http.Request) {
	if !b.hasPermission("settings.read") {
		http.Error(w, "settings.read permission is required", http.StatusForbidden)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/v1/settings/")
	if !extensionSecretNameRE.MatchString(name) {
		http.Error(w, "invalid setting name", http.StatusBadRequest)
		return
	}
	allowed := false
	for _, setting := range b.manifest.Settings {
		if setting.ID == name {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "setting is not declared by this extension", http.StatusForbidden)
		return
	}
	values, err := readExtensionSettings(b.manifest.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeBridgeJSON(w, map[string]string{"value": values[name]})
}

func (b *extensionBridge) workspace(w http.ResponseWriter) {
	if !b.hasPermission("workspace.read") {
		http.Error(w, "workspace.read permission is required", http.StatusForbidden)
		return
	}
	writeBridgeJSON(w, map[string]string{"cwd": b.cwd})
}

func (b *extensionBridge) secrets(w http.ResponseWriter, r *http.Request) {
	if !extensionSecretNameRE.MatchString(strings.TrimPrefix(r.URL.Path, "/v1/secrets/")) {
		http.Error(w, "invalid secret name", http.StatusBadRequest)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/v1/secrets/")
	switch r.Header.Get("X-Burrow-Secret-Operation") {
	case "get":
		if !b.hasPermission("secrets.read") {
			http.Error(w, "secrets.read permission is required", http.StatusForbidden)
			return
		}
		value, err := extensionKeychainGet(b.manifest.ID, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeBridgeJSON(w, map[string]string{"value": value})
	case "set":
		if !b.hasPermission("secrets.write") {
			http.Error(w, "secrets.write permission is required", http.StatusForbidden)
			return
		}
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := extensionKeychainSet(b.manifest.ID, name, body.Value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeBridgeJSON(w, map[string]bool{"ok": true})
	case "delete":
		if !b.hasPermission("secrets.write") {
			http.Error(w, "secrets.write permission is required", http.StatusForbidden)
			return
		}
		if err := extensionKeychainDelete(b.manifest.ID, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeBridgeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "set X-Burrow-Secret-Operation to get, set, or delete", http.StatusBadRequest)
	}
}

func (b *extensionBridge) reportTask(w http.ResponseWriter, r *http.Request) {
	if !b.hasPermission("tasks.report") {
		http.Error(w, "tasks.report permission is required", http.StatusForbidden)
		return
	}
	var task struct {
		ID       string   `json:"id"`
		Title    string   `json:"title"`
		Status   string   `json:"status"`
		Progress *float64 `json:"progress,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil || task.ID == "" || task.Title == "" {
		http.Error(w, "task id and title are required", http.StatusBadRequest)
		return
	}
	if task.Progress != nil && (*task.Progress < 0 || *task.Progress > 1) {
		http.Error(w, "progress must be between 0 and 1", http.StatusBadRequest)
		return
	}
	if b.app != nil && b.app.ctx != nil {
		runtime.EventsEmit(b.app.ctx, "extension-task:"+b.manifest.ID, task)
	}
	writeBridgeJSON(w, map[string]bool{"ok": true})
}

func writeBridgeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func extensionKeychainService(extensionID string) string { return "burrow-extension/" + extensionID }

func extensionKeychainGet(extensionID, name string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", extensionKeychainService(extensionID), "-a", name, "-w").Output()
	if err != nil {
		return "", errors.New("secret was not found in Keychain")
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

func extensionKeychainSet(extensionID, name, value string) error {
	_ = extensionKeychainDelete(extensionID, name)
	if out, err := exec.Command("security", "add-generic-password", "-U", "-s", extensionKeychainService(extensionID), "-a", name, "-w", value).CombinedOutput(); err != nil {
		return fmt.Errorf("write Keychain secret: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func extensionKeychainDelete(extensionID, name string) error {
	_, err := exec.Command("security", "delete-generic-password", "-s", extensionKeychainService(extensionID), "-a", name).CombinedOutput()
	return err
}
