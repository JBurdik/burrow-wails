package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"burrow/internal/agentproc"
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
	burrowBinDir string
	sessionDir   string

	httpSrv        *HTTPServer
	httpSrvRunning bool

	configDirs        *ConfigDirs
	maxAgents         int
	burrowMcpMaxDepth int
}

const httpServerPort = 37892

// SetHttpEnabled starts/stops the remote HTTP+WS server (browser/remote
// access), matching the frontend's `set_http_enabled` invoke call.
func (a *App) SetHttpEnabled(enabled bool) error {
	if enabled == a.httpSrvRunning {
		return nil
	}
	if enabled {
		a.httpSrv = NewHTTPServer(a)
		go func() {
			if err := a.httpSrv.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", httpServerPort)); err != nil {
				log.Printf("http server: %v", err)
			}
		}()
	}
	a.httpSrvRunning = enabled
	return nil
}

// HttpServerStatus mirrors Settings.vue's local httpStatus shape exactly
// (camelCase — that's what the original Rust command actually returned).
type HttpServerStatus struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	TokenPath   string `json:"tokenPath"`
	Token       string `json:"token"`
	PairingCode string `json:"pairingCode"`
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
		if len(s.Token) >= 6 {
			s.PairingCode = strings.ToUpper(s.Token[:6])
		}
	}
	return s
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

	hookSrv, err := StartHookServer(ctx)
	if err != nil {
		log.Printf("hook server: %v", err)
		return
	}
	a.hookSrv = hookSrv
	if err := os.WriteFile(filepath.Join(dataDir, "hook.port"), []byte(fmt.Sprintf("%d", hookSrv.port)), 0o644); err != nil {
		log.Printf("write hook.port: %v", err)
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
