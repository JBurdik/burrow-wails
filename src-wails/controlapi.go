package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"burrow/internal/control"
)

// The app side of the control surface: adapters that give internal/control the
// capabilities it declares, a UI bridge that lets a verb ask the frontend to do
// something and wait for the answer, and the loopback HTTP transport the
// `burrow` CLI and burrow-mcp talk to.
//
// Auth: the loopback listener is reachable by every process on this machine, and
// `spawn` starts programs — so it takes the same bearer token treatment as the
// tailnet server. The token lives in <app-data>/control.token (0600); the CLI
// reads it from there, exactly as it already reads hook.port.

// uiActionTimeout bounds a verb waiting on the frontend. Long enough for a tab
// to open on a busy app, short enough that an agent blocked on a UI that never
// answers (window closed, JS exception) gets a real error instead of hanging.
const uiActionTimeout = 15 * time.Second

// --- dependency adapters -----------------------------------------------------

type gitRunner struct{ app *App }

func (g gitRunner) Run(cwd string, args []string) (string, string, int) {
	out := runCmd("git", cwd, args)
	return out.Stdout, out.Stderr, out.Code
}

type ghRunner struct{ app *App }

func (g ghRunner) Run(cwd string, args []string) (string, string, int) {
	out := runCmd("gh", cwd, args)
	return out.Stdout, out.Stderr, out.Code
}

type execRunner struct{}

func (execRunner) RunProgram(prog, cwd string, args []string) (string, string, int) {
	out := runCmd(prog, cwd, args)
	return out.Stdout, out.Stderr, out.Code
}

type ptyWriter struct{ app *App }

// WritePty converts text to the []int byte list the daemon protocol carries.
func (p ptyWriter) WritePty(ptyID, text string) error {
	data := make([]int, 0, len(text))
	for _, b := range []byte(text) {
		data = append(data, int(b))
	}
	return p.app.WritePty(ptyID, data)
}

type worktreeAdapter struct{ app *App }

func (w worktreeAdapter) Create(repoPath, name, path, branch, baseRef string) (int64, error) {
	ws, err := w.app.CreateWorktree(repoPath, name, path, branch, baseRef)
	return ws.ID, err
}

func (w worktreeAdapter) Remove(workspaceID int64, force bool) error {
	return w.app.RemoveWorktree(workspaceID, force)
}

// worktreesDirPref reads the user's worktree parent dir out of config.json
// (uiPrefs.worktreesDir) — the same value the New-worktree dialog uses, so a
// Manager-made worktree lands where a hand-made one would.
func worktreesDirPref() string {
	path, err := configFilePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		UIPrefs struct {
			WorktreesDir string `json:"worktreesDir"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return ""
	}
	return cfg.UIPrefs.WorktreesDir
}

// --- UI bridge ---------------------------------------------------------------

type uiAck struct {
	Result json.RawMessage
	Err    string
}

// uiBridge dispatches an action to the frontend and waits for its ack. One
// pending entry per in-flight action, keyed by a request id, so several verbs
// (a Manager spawning three agents at once) can be outstanding together —
// unlike the sidebar's single-slot request ref, where a burst would clobber.
type uiBridge struct {
	// emit delivers the action to the frontend. Injected rather than calling
	// emitAll directly: the Wails runtime needs a live app context, so a verb
	// invoked before startup finished (or in a test) would otherwise panic
	// inside the event system instead of failing as a timeout.
	emit    func(event string, payload any)
	mu      sync.Mutex
	pending map[string]chan uiAck
	seq     int64
}

func newUIBridge(app *App) *uiBridge {
	return &uiBridge{
		emit:    func(event string, payload any) { emitAll(app.ctx, event, payload) },
		pending: map[string]chan uiAck{},
	}
}

func (u *uiBridge) Do(ctx context.Context, action string, args map[string]any) (json.RawMessage, error) {
	u.mu.Lock()
	u.seq++
	id := fmt.Sprintf("ui%d", u.seq)
	ch := make(chan uiAck, 1)
	u.pending[id] = ch
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		delete(u.pending, id)
		u.mu.Unlock()
	}()

	if args == nil {
		args = map[string]any{}
	}
	u.emit("control:action", map[string]any{"id": id, "action": action, "args": args})

	select {
	case ack := <-ch:
		if ack.Err != "" {
			return nil, errors.New(ack.Err)
		}
		return ack.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(uiActionTimeout):
		return nil, fmt.Errorf("%s: the app did not respond within %s", action, uiActionTimeout)
	}
}

// AckControlAction is how the frontend answers a control:action event. Called
// with the action's JSON result, or errMsg when it could not be performed.
func (a *App) AckControlAction(id, resultJSON, errMsg string) {
	if a.ui == nil {
		return
	}
	a.ui.mu.Lock()
	ch := a.ui.pending[id]
	a.ui.mu.Unlock()
	if ch == nil {
		return // timed out already; nothing to deliver to
	}
	ch <- uiAck{Result: json.RawMessage(resultJSON), Err: errMsg}
}

// --- wiring ------------------------------------------------------------------

func (a *App) initControl(dataDir string) {
	a.ui = newUIBridge(a)
	a.control = control.New(control.Deps{
		DB:           a.db,
		SessionDir:   a.sessionDir,
		Git:          gitRunner{a},
		Gh:           ghRunner{a},
		Exec:         execRunner{},
		PTY:          ptyWriter{a},
		Worktrees:    worktreeAdapter{a},
		UI:           a.ui,
		WorktreesDir: worktreesDirPref,
	})
	a.controlToken = loadOrCreateToken(dataDir, "control.token")
}

// ControlVerbs exposes the registry to the frontend, which generates the
// Manager's primer from it — so the primer lists exactly the verbs that exist.
type ControlVerb struct {
	Name    string           `json:"name"`
	Summary string           `json:"summary"`
	Args    []ControlVerbArg `json:"args"`
}

type ControlVerbArg struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Desc     string `json:"desc"`
	Required bool   `json:"required"`
}

func (a *App) ControlVerbs() []ControlVerb {
	if a.control == nil {
		return []ControlVerb{}
	}
	out := []ControlVerb{}
	for _, v := range a.control.Verbs() {
		cv := ControlVerb{Name: v.Name, Summary: v.Summary, Args: []ControlVerbArg{}}
		for _, arg := range v.Args {
			cv.Args = append(cv.Args, ControlVerbArg{Name: arg.Name, Type: arg.Type, Desc: arg.Desc, Required: arg.Required})
		}
		out = append(out, cv)
	}
	return out
}

// --- loopback transport ------------------------------------------------------

// registerControlRoutes mounts the control API on the loopback hook server:
// POST /v1/<verb> with a JSON body, plus /v1/_verbs for the registry. The
// frontend's replies don't come back this way — it acks over its Wails binding
// (AckControlAction), which needs no token and no port.
func (a *App) registerControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !a.controlAuthorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		verb := strings.TrimPrefix(r.URL.Path, "/v1/")
		if verb == "_verbs" {
			writeJSONFileResponse(w, a.ControlVerbs())
			return
		}

		var params control.Params
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil && err.Error() != "EOF" {
			http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if a.control == nil {
			http.Error(w, "control surface not ready", http.StatusServiceUnavailable)
			return
		}

		// A verb's own deadline is its business (wait_result blocks for minutes);
		// the request context only carries client disconnects.
		result, err := a.control.Call(r.Context(), control.ScopeLocal, verb, params)
		if err != nil {
			status := http.StatusInternalServerError
			var unknown control.ErrUnknownVerb
			if errors.As(err, &unknown) {
				status = http.StatusNotFound
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		writeJSONFileResponse(w, result)
	})
}

func writeJSONFileResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) controlAuthorized(r *http.Request) bool {
	if a.controlToken == "" {
		return false // token unavailable → fail closed
	}
	got := r.Header.Get("Authorization")
	want := "Bearer " + a.controlToken
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// loadOrCreateToken persists a random bearer token under the app data dir.
func loadOrCreateToken(dataDir, name string) string {
	path := filepath.Join(dataDir, name)
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b))
	}
	token := randomHex(24)
	if token == "" {
		return ""
	}
	_ = os.WriteFile(path, []byte(token), 0o600)
	return token
}

// --- MCP injection -----------------------------------------------------------

// burrowMcpServers adds Burrow's own MCP server to a session's server map, so a
// chat agent gets the control verbs as typed tools (with schemas) instead of
// having to remember the CLI's flags. The CLI stays the universal path — this is
// the same verbs through a nicer door, for the clients that support MCP.
//
// Skipped silently when the binary isn't next to the app (a `wails dev` run
// builds no sidecar): the agent still has `burrow` on its PATH.
func burrowMcpServers(existing map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range existing {
		out[k] = v
	}
	bin := burrowMcpBinary()
	if bin == "" {
		return out
	}
	out["burrow"] = map[string]any{"type": "stdio", "command": bin, "args": []string{}}
	return out
}

// burrowMcpBinary is the sidecar's path inside the app bundle
// (Burrow.app/Contents/MacOS/burrow-mcp), or "" when it isn't there.
func burrowMcpBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	path := filepath.Join(filepath.Dir(exe), "burrow-mcp")
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

// acpMcpServers is the same injection in ACP's shape: session/new takes a LIST
// of servers, each with its name inline, rather than Claude's name-keyed map.
func acpMcpServers() []any {
	bin := burrowMcpBinary()
	if bin == "" {
		return []any{}
	}
	return []any{map[string]any{"name": "burrow", "command": bin, "args": []string{}, "env": []any{}}}
}

// addBurrowEnv puts the `burrow` CLI on an agent's PATH and tells it where the
// app is: without this an ACP agent (Codex, Gemini) can't reach the control API
// at all, so it could never act as a Manager. No BURROW_PTY_ID — a chat is not a
// tab, so the global status hook stays a no-op for it, same as claudechat.go.
func (a *App) addBurrowEnv(env map[string]string, cwd string) {
	path := augmentedPath(cwd)
	if a.burrowBinDir != "" {
		path = a.burrowBinDir + string(os.PathListSeparator) + path
	}
	env["PATH"] = path
	if a.sessionDir != "" {
		env["BURROW_SESSION_DIR"] = a.sessionDir
	}
	env["BURROW_CWD"] = cwd
	if dir, err := appDataDir(); err == nil {
		env["BURROW_HOME_DIR"] = dir
	}
	if a.hookSrv != nil {
		env["BURROW_HOOK_PORT"] = fmt.Sprintf("%d", a.hookSrv.port)
	}
}
