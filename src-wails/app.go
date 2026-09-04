package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"burrow/internal/agentproc"
	"burrow/internal/control"
)

// App is the Wails-bound struct exposing methods to the frontend, replacing
// the #[tauri::command] surface in src-tauri/src/lib.rs.
type App struct {
	ctx    context.Context
	db     *sql.DB
	daemon *DaemonClient

	streamOnce sync.Once
	streamW    *chatStreamWriter

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

	// Chat transcripts used to live in config.json; move them into SQLite before
	// the frontend reads either store.
	a.migrateChatHistoryToSQLite()

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

	// Warm the font list so the Settings pickers don't pay the ~1 s scan.
	go ListFonts()

	go a.reapIdleAgents()

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
	if err := a.daemon.CreatePty(id, cwd, cols, rows, env); err != nil {
		return err
	}
	// An externally spawned terminal may have sent its first hook before this
	// frontend view attached. Replay the cached state now that XTerm is listening.
	if a.hookSrv != nil {
		a.hookSrv.ReplayStatus(id)
	}
	return nil
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

// GetPtyForeground names the command in the foreground of a PTY. It is the
// second of the two channels that decide a status dot (`XTerm.vue`): the agent
// hooks are authoritative for an agent, and this poll covers everything they
// cannot see — a plain `npm test` that has no hooks, and an agent that was
// Ctrl-C'd without emitting a Stop.
//
// A failure is reported as an empty name, not an error: the caller polls this
// every 2 s and already treats "" as "nothing to say", whereas a rejected
// promise would spam the console on every teardown race.
func (a *App) GetPtyForeground(id string) string {
	name, err := a.daemon.Foreground(id)
	if err != nil {
		return ""
	}
	return name
}

// Idle-agent reaping, modelled on t3code's ProviderSessionReaper (same
// thresholds): a chat's CLI is only worth its ~150 MB while something is
// happening on it. Killing it emits the usual exit event, so the frontend marks
// the chat cold and the next prompt spawns a replacement with --resume.
const (
	agentIdleThreshold = 30 * time.Minute
	agentSweepInterval = 5 * time.Minute
)

func (a *App) reapIdleAgents() {
	for range time.Tick(agentSweepInterval) {
		if a.claudeAgents == nil {
			continue
		}
		if reaped := a.claudeAgents.ReapIdle(agentIdleThreshold); len(reaped) > 0 {
			log.Printf("reaped %d idle agent session(s): %v", len(reaped), reaped)
		}
	}
}

// cleanupOnShutdown kills every agent CLI subprocess we started. Without it
// each chat session's `claude`/adapter process outlives the app as an orphan
// (AgentChat.vue deliberately does not stop the proc on unmount, so nothing
// else does it either) and they pile up across launches.
//
// ponytail: PTYs are deliberately NOT touched — the daemon owns them and
// keeping them alive across a restart is the reattach feature.
func (a *App) cleanupOnShutdown() {
	if a.claudeAgents != nil {
		a.claudeAgents.StopAll()
	}
	if a.acpSessions != nil {
		for _, id := range a.acpSessions.ids() {
			_ = a.AcpStop(id)
		}
	}
}
