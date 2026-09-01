package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"burrow/internal/agentproc"
	"burrow/internal/control"
)

// App is the Wails-bound struct exposing methods to the frontend, replacing
// the #[tauri::command] surface in src-tauri/src/lib.rs.
type App struct {
	ctx    context.Context
	db     *sql.DB
	daemon *DaemonClient

	claudeAgents *agentproc.Manager
	acpSessions  *acpRegistry
	lspMgr       *lspManager

	hookSrv      *HookServer
	control      *control.Core
	ui           *uiBridge
	controlToken string
	burrowBinDir string
	sessionDir   string

	httpSrv        *HTTPServer
	httpSrvRunning bool

	maxAgents         int
	burrowMcpMaxDepth int
}

const httpServerPort = 37892

// httpEnabledPrefPath is the marker file that survives a restart. Its
// presence is the whole pref — the Rust backend used the same
// `http_enabled` file for this.
func httpEnabledPrefPath() (string, error) {
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "http_enabled"), nil
}

// SetHttpEnabled starts/stops the remote HTTP+WS server (browser/remote
// access), matching the frontend's `set_http_enabled` invoke call, and
// persists the choice so it survives a restart.
func (a *App) SetHttpEnabled(enabled bool) error {
	if err := a.setHttpEnabled(enabled); err != nil {
		return err
	}
	path, err := httpEnabledPrefPath()
	if err != nil {
		return err
	}
	if enabled {
		return os.WriteFile(path, []byte("1"), 0o644)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// setHttpEnabled is the in-process half, without touching the pref file —
// startup() calls it directly when restoring the persisted state.
func (a *App) setHttpEnabled(enabled bool) error {
	if enabled == a.httpSrvRunning {
		return nil
	}
	if enabled {
		a.httpSrv = NewHTTPServer(a)
		// Publish it so emitAll fans events out to browser clients too.
		wsBroadcaster.Store(a.httpSrv)
		srv := a.httpSrv
		go func() {
			if err := srv.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", httpServerPort)); err != nil && err != http.ErrServerClosed {
				log.Printf("http server: %v", err)
			}
		}()
	} else {
		wsBroadcaster.Store(nil)
		// Actually close the listener. "Remote access: off" that leaves the
		// port open until the next restart is not off.
		if a.httpSrv != nil {
			if err := a.httpSrv.Close(); err != nil {
				log.Printf("http server close: %v", err)
			}
			a.httpSrv = nil
		}
	}
	a.httpSrvRunning = enabled
	return nil
}

// HttpServerStatus mirrors Settings.vue's local httpStatus shape exactly
// (camelCase — that's what the original Rust command actually returned).
type HttpServerStatus struct {
	Enabled   bool   `json:"enabled"`
	Port      int    `json:"port"`
	TokenPath string `json:"tokenPath"`
	Token     string `json:"token"`
	// PairCode is the six-digit code the phone types on the Connect screen.
	// Empty while pairing is locked out after too many wrong guesses.
	PairCode   string `json:"pairCode"`
	PairLocked bool   `json:"pairLocked"`
}

func (a *App) GetHttpServerStatus() HttpServerStatus {
	dataDir, _ := appDataDir()
	s := HttpServerStatus{
		Enabled:   a.httpSrvRunning,
		Port:      httpServerPort,
		TokenPath: filepath.Join(dataDir, "http.token"),
	}
	if a.httpSrv != nil {
		s.Token = a.httpSrv.token
		s.PairCode = a.httpSrv.PairCode()
		s.PairLocked = s.PairCode == ""
	}
	return s
}

// RegeneratePairCode issues a fresh pairing code and clears the lockout.
func (a *App) RegeneratePairCode() string {
	if a.httpSrv == nil {
		return ""
	}
	return a.httpSrv.RegeneratePairCode()
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dataDir, err := appDataDir()
	if err != nil {
		log.Printf("app data dir: %v", err)
		return
	}
	db, err := openDB(dataDir)
	if err != nil {
		log.Printf("open db: %v", err)
		return
	}
	a.db = db

	a.daemon = NewDaemonClient(ctx, filepath.Join(dataDir, "daemon.sock"))
	if err := a.daemon.Ensure(); err != nil {
		log.Printf("daemon: %v", err)
	}

	binDir, err := ensureBurrowBin(dataDir)
	if err != nil {
		log.Printf("ensure burrow bin: %v", err)
	}
	a.burrowBinDir = binDir
	a.sessionDir = filepath.Join(dataDir, "sessions")
	_ = os.MkdirAll(a.sessionDir, 0o755)

	// Global status hooks: what gives every agent session (not just spawned
	// ones) a status dot. Idempotent, so running it every launch also repairs
	// a config the user or another tool edited.
	installStatusHooks(dataDir)
	installAgentDocs()

	a.initControl(dataDir)

	hookSrv, err := StartHookServer(ctx, a.registerControlRoutes)
	if err != nil {
		log.Printf("hook server: %v", err)
		return
	}
	a.hookSrv = hookSrv
	if err := os.WriteFile(filepath.Join(dataDir, "hook.port"), []byte(fmt.Sprintf("%d", hookSrv.port)), 0o644); err != nil {
		log.Printf("write hook.port: %v", err)
	}

	// Restore remote access if it was left on. Without this the Settings
	// toggle silently reset to off on every launch.
	if path, err := httpEnabledPrefPath(); err == nil {
		if _, err := os.Stat(path); err == nil {
			if err := a.setHttpEnabled(true); err != nil {
				log.Printf("restore http server: %v", err)
			}
		}
	}
}

func (a *App) GetHookServerPort() int {
	if a.hookSrv == nil {
		return 0
	}
	return a.hookSrv.port
}

func appDataDir() (string, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Library", "Application Support", "burrow-wails"), nil
}

// --- PTY bindings (proxy to burrow-daemon) ---
// Signature matches src-tauri's create_pty(id, cwd, cols, rows, ...) exactly
// — the frontend (XTerm.vue) owns its own numeric pty-id counter and always
// passes it in; the backend never generates one.

func (a *App) CreatePty(id string, cwd string, cols, rows uint16) error {
	env := []string{
		"PATH=" + a.burrowBinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"BURROW_SESSION_DIR=" + a.sessionDir,
		"BURROW_CWD=" + cwd,
	}
	if a.hookSrv != nil {
		env = append(env, fmt.Sprintf("BURROW_HOOK_PORT=%d", a.hookSrv.port))
	}
	return a.daemon.CreatePty(id, cwd, cols, rows, env)
}

func (a *App) WritePty(id string, data []int) error {
	return a.daemon.Write(id, data)
}

func (a *App) ResizePty(id string, cols, rows uint16) error {
	return a.daemon.Resize(id, cols, rows)
}

func (a *App) KillPty(id string) error {
	return a.daemon.Kill(id)
}

func (a *App) ListPtySessions() ([]string, error) {
	return a.daemon.List()
}
