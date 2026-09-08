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

	"burrow/internal/agentphase"
	"burrow/internal/agentproc"
	"burrow/internal/control"
)

// App is the Wails-bound struct exposing methods to the frontend, replacing
// the #[tauri::command] surface in src-tauri/src/lib.rs.
type App struct {
	ctx    context.Context
	db     *sql.DB
	daemon *DaemonClient
	phases *PhaseStore
	poller *phasePoller

	streamOnce sync.Once
	streamW    *chatStreamWriter

	claudeAgents *agentproc.Manager
	acpSessions  *acpRegistry
	lspMgr       *lspManager

	hookSrv       *HookServer
	control       *control.Core
	ui            *uiBridge
	controlToken  string
	burrowBinDir  string
	sessionDir    string
	environmentID string

	endpointProviders []EndpointProvider

	tickets *ticketStore
	// remoteWS is held so RevokeRemoteDevice can reach the live connections
	// and so the tailnet server can mount the SAME handler with the SAME
	// ticket store — a second store would mean a ticket minted by
	// /v2/ws-ticket is unknown to the handler that redeems it.
	remoteWS *remoteWS
	// remoteAuth owns the pairing code and the /v2/pair + /v2/ws-ticket hops.
	// One instance for the app, not one per listener: the code the user reads
	// in Settings has to be the code the phone types.
	remoteAuth *remoteAuth
	// hookPort mirrors hookSrv.port, assigned once at startup. It exists as
	// its own field so LocalEndpoint is testable without standing up a real
	// hook server; hookSrv stays the source of truth everywhere else.
	hookPort int

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
		addr := fmt.Sprintf("127.0.0.1:%d", httpServerPort)
		// Both guards fail CLOSED, before the listener exists (spec §4
		// invariants 1 and 2). A warning in a log nobody reads is not a
		// guard: funnel would publish this exact handler — same host, same
		// :443, same /burrow path — on the open internet, where a six-digit
		// pairing code is not a defence.
		if err := assertLoopbackAddr(addr); err != nil {
			return err
		}
		funnel := a.funnelEnabled
		if funnelCheckHook != nil {
			funnel = funnelCheckHook
		}
		if funnel() {
			return fmt.Errorf("remote access refused: `tailscale funnel` is on for this node, which would publish Burrow on the public internet. Turn funnel off (`tailscale funnel off`) and try again")
		}
		// No bus sink to publish any more: a remote client subscribes to the
		// bus through its own /v2/ws connection, exactly like the desktop's,
		// so there is nothing for this listener to fan out.
		a.httpSrv = NewHTTPServer(a)
		srv := a.httpSrv
		go func() {
			// The very addr assertLoopbackAddr just cleared — not a second
			// format string that could drift away from the one checked.
			if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
				log.Printf("http server: %v", err)
			}
		}()
	} else {
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

// HttpServerStatus is what Settings reads to render the remote-access block.
//
// There is no Token field any more. The shared http.token is gone (phase 6):
// every device has its own, none of them is readable after pairing, and the
// pairing code lives in RemotePairStatus. A field here that showed a
// credential was also a credential in a screenshot.
type HttpServerStatus struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
}

func (a *App) GetHttpServerStatus() HttpServerStatus {
	return HttpServerStatus{Enabled: a.httpSrvRunning, Port: httpServerPort}
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// The bus has exactly ONE sink now: remotews.handle's per-connection
	// subscription. It is still the single door for every event a client may
	// care about — what went away is the second delivery path, not the door.
	//
	// The v1 tailnet broadcaster is gone with the client that needed it, and
	// there is deliberately no bus -> Wails-runtime sink either: every src/
	// subscription that is not one of the desktop-only names (menu-*,
	// lsp-msg-*, float-*, extension-task:*, update:*, all emitted with
	// runtime.EventsEmit directly and on events_test.go's allowlist) goes
	// through src/lib/wailsCompat/event.ts to the socket, and the desktop's
	// own connection subscribes for itself. Feeding the webview as well
	// marshalled every bus event — including every pty-data-<id> chunk —
	// across the JS bridge for no consumer.

	dataDir, err := appDataDir()
	if err != nil {
		log.Printf("app data dir: %v", err)
		return
	}
	if id, err := environmentID(dataDir); err != nil {
		log.Printf("environment id: %v", err)
	} else {
		a.environmentID = id
	}
	a.endpointProviders = []EndpointProvider{
		newLoopbackProvider(httpServerPort),
		newTailscaleProvider(a.GetTailscaleStatus),
	}
	db, err := openDB(dataDir)
	if err != nil {
		log.Printf("open db: %v", err)
		return
	}
	a.db = db
	if ps, err := NewPhaseStore(db); err != nil {
		log.Printf("phase store: %v", err)
	} else {
		a.phases = ps
	}

	// Chat transcripts used to live in config.json; move them into SQLite before
	// the frontend reads either store.
	a.migrateChatHistoryToSQLite()
	// Before anything can serve a client: the chat LIST moves out of
	// config.json here, and a client that read the old key first would then
	// write it back over the new source of truth.
	a.migrateChatsFromConfig()

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

	// Hard cutover (spec §4): the shared tailnet token is deleted rather than
	// migrated. A token with no scopes, no device identity and no revocation
	// cannot be translated honestly into a scoped per-device session, and
	// leaving the file on disk would leave a credential nothing reads and
	// nobody can revoke. Devices paired against it must pair again — which is
	// also true because the surface it authenticated no longer exists.
	if err := os.Remove(filepath.Join(dataDir, "http.token")); err == nil {
		log.Printf("removed the legacy shared http.token; devices pair per-device now")
	}

	a.tickets = newTicketStore()
	a.remoteWS = newRemoteWS(a, a.tickets)
	a.remoteAuth = newRemoteAuth(a, a.tickets)
	hookSrv, err := StartHookServer(ctx, a.phases, a.registerControlRoutes, a.remoteWS.register)
	if err != nil {
		log.Printf("hook server: %v", err)
		return
	}
	a.hookSrv = hookSrv
	a.hookPort = a.hookSrv.port
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

	if a.phases != nil {
		a.poller = startPhasePoll(ctx, a.phases, a.ListPtySessions, a.GetPtyForeground)
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
	// Fresh spawn or reattach? The daemon already knows: it holds every live
	// pty, so an id it does not list is a brand-new one. That distinction is
	// the whole pty lifecycle we have — ids are REUSED (the frontend's counter
	// reseeds from max(saved, daemon-alive) on restart), so without it a new
	// "Terminal 2" inherits the phase of whatever held id 2 last: a green
	// review dot for a turn that ended days ago, and that session's task title
	// pasted over the tab name.
	// A failed List means we do not KNOW, and the conservative answer is
	// "reattach": keeping a phase we should have dropped costs a stale dot
	// until the next hook, dropping one we should have kept loses a live
	// agent's state outright.
	fresh := false
	if live, err := a.daemon.List(); err == nil {
		fresh = true
		for _, s := range live {
			if s == id {
				fresh = false
				break
			}
		}
	}

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
	if fresh {
		if a.phases != nil {
			a.phases.Forget("pty:" + id)
		}
		if a.poller != nil {
			a.poller.forget(id)
		}
		if a.hookSrv != nil {
			a.hookSrv.ForgetStatus(id)
		}
	}
	// An externally spawned terminal may have sent its first hook before this
	// frontend view attached. Replay the cached state now that XTerm is listening.
	if a.hookSrv != nil {
		a.hookSrv.ReplayStatus(id)
	}
	return nil
}

// ctrlC / escByte are the two keystrokes that cancel an agent turn.
const (
	ctrlC   = 0x03
	escByte = 0x1b
)

// WritePty forwards keystrokes to the PTY, and watches for the one keystroke
// that is also a phase event.
//
// Cancelling a turn fires NO Stop hook, and the foreground poll cannot settle
// an agent either — an agent is foreground whether it is thinking or idle at
// its prompt, which is exactly why agentphase.Next refuses to let the poll
// speak for one. The dead-PTY watchdog can't help either: the PTY is alive.
// So this write is the only evidence the turn ended, and without it the dot
// sticks orange until the next turn starts.
//
// It lives here rather than in XTerm.vue's onData (where it used to) so the
// phase stays derivable server-side — phase 4's phone gets it for free.
// A lone 0x03/0x1b only: arrow keys and every other escape sequence arrive as
// ESC plus more bytes in one write, so length is what separates a cancel from
// a cursor key.
func (a *App) WritePty(id string, data []int) error {
	if a.phases != nil && len(data) == 1 && (data[0] == ctrlC || data[0] == escByte) {
		a.phases.Apply("pty:"+id, agentphase.Event{Kind: agentphase.Interrupt})
	}
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

// LocalEndpointInfo is the desktop's bootstrap: where to connect and the
// one-shot credential to connect with.
type LocalEndpointInfo struct {
	WSURL         string `json:"ws_url"`
	Ticket        string `json:"ticket"`
	EnvironmentID string `json:"environment_id"`
}

// LocalEndpoint hands the desktop frontend a fresh single-use ticket for
// /v2/ws. This is the one thing the desktop still needs a Wails binding for,
// and the reason it needs one: being in-process IS the desktop's
// authorization, and that is not a claim anything on the network can make.
// The frontend calls this again on every reconnect, since a ticket is spent
// by the handshake that uses it.
func (a *App) LocalEndpoint() LocalEndpointInfo {
	if a.tickets == nil {
		return LocalEndpointInfo{}
	}
	// The desktop is the app; it gets every scope — including scopeUIAck,
	// which nothing else may ever hand out. That scope is not authority, it
	// is the claim "I am the UI a control verb is waiting for" (see its
	// comment in remoteapi.go); being in-process is the only thing that
	// substantiates it, and this is the only issuer that knows it.
	all := []remoteScope{scopeOrchRead, scopeOrchOperate, scopeTerminal, scopeAccessRead, scopeAccessWrite, scopeUIAck}
	return LocalEndpointInfo{
		WSURL:         fmt.Sprintf("ws://127.0.0.1:%d/v2/ws", a.hookPort),
		Ticket:        a.tickets.issue(all),
		EnvironmentID: a.environmentID,
	}
}
