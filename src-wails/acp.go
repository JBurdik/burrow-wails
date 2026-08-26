package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ACP / Codex app-server bridge. Ports acp_start/codex_start/acp_send/
// acp_set_mode/acp_set_config/acp_list_sessions/acp_stop from
// src-tauri/src/lib.rs.
//
// Both runtimes speak newline-delimited JSON-RPC on stdio. This file owns the
// handshake (so the frontend gets one `_burrow:"session"` line carrying
// sessionId + modes + configOptions, which drives the model / mode selectors)
// and then pumps every further line to the frontend:
//   server→client REQUEST (has method AND id) → `acp-req-{id}`  (permissions)
//   everything else                           → `acp-data-{id}`
// {id} is the FRONTEND's chat id — ClaudeChat.vue listens on exactly that name.

type acpProtocol int

const (
	protoACP acpProtocol = iota
	protoCodexAppServer
)

type acpSession struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	proto     acpProtocol
	sessionID string // ACP sessionId / Codex threadId

	mu          sync.Mutex
	nextID      int64
	pendingTurn int64  // Codex: rpc id of the turn awaiting turn/completed (0 = none)
	model       string // Codex: model override applied on the next turn/start
	effort      string
}

func (s *acpSession) rpcID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}

func (s *acpSession) write(msg any) error {
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.stdin.Write(append(line, '\n'))
	return err
}

type acpRegistry struct {
	mu   sync.Mutex
	live map[string]*acpSession
}

func (r *acpRegistry) get(id string) (*acpSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.live[id]
	return s, ok
}

func (r *acpRegistry) put(id string, s *acpSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live == nil {
		r.live = map[string]*acpSession{}
	}
	r.live[id] = s
}

func (r *acpRegistry) drop(id string) *acpSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.live[id]
	delete(r.live, id)
	return s
}

func (a *App) acpReg() *acpRegistry {
	if a.acpSessions == nil {
		a.acpSessions = &acpRegistry{live: map[string]*acpSession{}}
	}
	return a.acpSessions
}

// AcpStartOpts mirrors ClaudeChat.vue's acpStartPayload().
type AcpStartOpts struct {
	ID              string            `json:"id"`
	Cwd             string            `json:"cwd"`
	Command         string            `json:"command"` // adapter program: npx, gemini, codex, opencode…
	Args            []string          `json:"args"`
	Env             map[string]string `json:"env"`
	Kind            string            `json:"kind"` // claude|gemini|codex|custom — drives env injection
	ConfigDir       string            `json:"configDir"`
	EnvFile         string            `json:"envFile"`
	ResumeSessionID string            `json:"resumeSessionId"`
	EmitHistory     bool             `json:"emitHistory"`
}

// jsonRPCReader reads newline JSON from the child and can wait for one response id.
type jsonRPCReader struct {
	sc *bufio.Scanner
}

func newJSONRPCReader(r io.Reader) *jsonRPCReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	return &jsonRPCReader{sc: sc}
}

// await reads until the response with id `want` arrives. Every line seen on the
// way is handed to onOther (used to forward a session/load history replay).
func (j *jsonRPCReader) await(want int64, onOther func(raw string, msg map[string]any)) (map[string]any, error) {
	for j.sc.Scan() {
		raw := strings.TrimSpace(j.sc.Text())
		if raw == "" {
			continue
		}
		var msg map[string]any
		if json.Unmarshal([]byte(raw), &msg) != nil {
			continue
		}
		if _, isReq := msg["method"]; !isReq {
			if idOf(msg["id"]) == want {
				return msg, nil
			}
		}
		if onOther != nil {
			onOther(raw, msg)
		}
	}
	if err := j.sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("agent closed during handshake")
}

func idOf(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return -1
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// loadDotenv reads key=value pairs, ignoring comments/blank lines.
func loadDotenv(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// spawnStdio starts a JSON-RPC child with piped stdio. stderr is drained and its
// tail kept, so a failed handshake can report the real cause.
func spawnStdio(bin string, args []string, cwd string, env map[string]string) (*exec.Cmd, io.WriteCloser, *jsonRPCReader, func() string, error) {
	c := exec.Command(bin, args...)
	c.Dir = cwd
	c.Env = append(os.Environ(), envSlice(env)...)
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := c.Start(); err != nil {
		return nil, nil, nil, nil, err
	}
	var mu sync.Mutex
	tail := ""
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				mu.Lock()
				tail += string(buf[:n])
				if len(tail) > 8192 {
					tail = tail[len(tail)-8192:]
				}
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return c, stdin, newJSONRPCReader(stdout), func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.TrimSpace(tail)
	}, nil
}

// pump forwards the rest of the child's output for the session's lifetime.
func (a *App) pump(chatID string, r *jsonRPCReader, sess *acpSession) {
	for r.sc.Scan() {
		raw := strings.TrimSpace(r.sc.Text())
		if raw == "" {
			continue
		}
		var msg map[string]any
		if json.Unmarshal([]byte(raw), &msg) != nil {
			continue
		}
		if sess.proto == protoCodexAppServer {
			a.pumpCodexLine(chatID, msg, sess)
			continue
		}
		// A message with BOTH method and id is a server→client request
		// (session/request_permission); everything else is data.
		_, hasMethod := msg["method"]
		_, hasID := msg["id"]
		topic := "acp-data-" + chatID
		if hasMethod && hasID {
			topic = "acp-req-" + chatID
		}
		runtime.EventsEmit(a.ctx, topic, raw)
	}
	runtime.EventsEmit(a.ctx, "acp-data-"+chatID, `{"_burrow":"exit"}`)
}

// pumpCodexLine translates Codex app-server notifications into the ACP
// session/update shape the frontend already renders.
func (a *App) pumpCodexLine(chatID string, msg map[string]any, sess *acpSession) {
	method, _ := msg["method"].(string)
	params := mapOf(msg["params"])
	emit := func(v any) {
		line, err := json.Marshal(v)
		if err != nil {
			return
		}
		runtime.EventsEmit(a.ctx, "acp-data-"+chatID, string(line))
	}
	switch method {
	case "item/agentMessage/delta":
		delta, _ := params["delta"].(string)
		if delta == "" {
			return
		}
		messageID, _ := params["itemId"].(string)
		if messageID == "" {
			messageID = "codex-message"
		}
		emit(map[string]any{"method": "session/update", "params": map[string]any{
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"messageId":     messageID,
				"content":       map[string]any{"text": delta},
			}}})
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		delta, _ := params["delta"].(string)
		if delta == "" {
			return
		}
		emit(map[string]any{"method": "session/update", "params": map[string]any{
			"update": map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]any{"text": delta},
			}}})
	case "turn/completed":
		sess.mu.Lock()
		rpc := sess.pendingTurn
		sess.pendingTurn = 0
		sess.mu.Unlock()
		if rpc != 0 {
			emit(map[string]any{"id": rpc, "result": map[string]any{}})
		}
	default:
		if _, hasID := msg["id"]; hasID && method != "" {
			line, err := json.Marshal(msg)
			if err == nil {
				runtime.EventsEmit(a.ctx, "acp-req-"+chatID, string(line))
			}
		}
	}
}

// emitSession hands the frontend the one line its selectors populate from.
func (a *App) emitSession(chatID, sessionID string, modes any, configOptions any) {
	if configOptions == nil {
		configOptions = []any{}
	}
	line, err := json.Marshal(map[string]any{
		"_burrow":       "session",
		"sessionId":     sessionID,
		"modes":         modes,
		"configOptions": configOptions,
	})
	if err != nil {
		return
	}
	runtime.EventsEmit(a.ctx, "acp-data-"+chatID, string(line))
}

// AcpStart spawns an ACP adapter for chat opts.ID and completes its handshake.
func (a *App) AcpStart(opts AcpStartOpts) error {
	if _, live := a.acpReg().get(opts.ID); live {
		return nil
	}
	if opts.Kind == "codex" && opts.Command == "codex" {
		return a.CodexStart(opts.ID, opts.Cwd, opts.Env, opts.ResumeSessionID)
	}

	env := map[string]string{}
	for k, v := range opts.Env {
		env[k] = v
	}
	switch opts.Kind {
	case "claude":
		// The claude ACP adapter shells out to the `claude` binary: point it at the
		// resolved path and blank ANTHROPIC_API_KEY so subscription OAuth is used.
		bin := resolveAgentBin("claude", opts.Cwd)
		if bin == "" {
			return fmt.Errorf("claude binary not found (checked ~/.local/bin, homebrew, PATH)")
		}
		env["CLAUDE_CODE_EXECUTABLE"] = bin
		env["ANTHROPIC_API_KEY"] = ""
		if cd := strings.TrimSpace(opts.ConfigDir); cd != "" {
			env["CLAUDE_CONFIG_DIR"] = cd
		}
	case "codex":
		for _, k := range []string{"CODEX_API_KEY", "OPENAI_API_KEY", "OPEN_AI_API_KEY"} {
			if _, set := env[k]; !set {
				if v := os.Getenv(k); v != "" {
					env[k] = v
				}
			}
		}
	}

	bin := resolveAgentBin(opts.Command, opts.Cwd)
	if bin == "" {
		return fmt.Errorf("%s binary not found (checked ~/.local/bin, homebrew, PATH)", opts.Command)
	}
	args := append([]string{}, opts.Args...)
	// npx with the JSON-RPC pipe on stdin would block on its install confirmation
	// (reading our handshake as the answer) — force non-interactive.
	if base := filepath.Base(bin); base == "npx" || base == "npx.cmd" {
		hasYes := false
		for _, x := range args {
			if x == "-y" || x == "--yes" {
				hasYes = true
			}
		}
		if !hasYes {
			args = append([]string{"-y"}, args...)
		}
	}
	envFile := opts.EnvFile
	if envFile == "" {
		envFile = ".env"
	}
	for k, v := range loadDotenv(filepath.Join(opts.Cwd, envFile)) {
		if _, set := env[k]; !set {
			env[k] = v
		}
	}
	env["PATH"] = augmentedPath(opts.Cwd)

	cmd, stdin, reader, stderrTail, err := spawnStdio(bin, args, opts.Cwd, env)
	if err != nil {
		return fmt.Errorf("failed to spawn acp adapter: %w", err)
	}
	sess := &acpSession{cmd: cmd, stdin: stdin, proto: protoACP, nextID: 100}

	// Watchdog: a hung handshake would otherwise leave the chat "thinking"
	// forever with no model and no error. A cold `npx -y` download is slow, so
	// give it 120 s before killing the adapter (which unblocks the reader).
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-time.After(120 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	fail := func(err error) error {
		close(done)
		_ = cmd.Process.Kill()
		if tail := stderrTail(); tail != "" {
			return fmt.Errorf("%w\n--- adapter stderr ---\n%s", err, tail)
		}
		return err
	}

	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": 1,
			"clientCapabilities": map[string]any{
				"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
				"terminal": false,
			},
			"clientInfo": map[string]any{"name": "burrow", "title": "Burrow", "version": appVersion},
		},
	}); err != nil {
		return fail(err)
	}
	if _, err := reader.await(0, nil); err != nil {
		return fail(err)
	}

	var result map[string]any
	sessionID := ""
	if sid := strings.TrimSpace(opts.ResumeSessionID); sid != "" {
		if err := sess.write(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "session/load",
			"params": map[string]any{"sessionId": sid, "cwd": opts.Cwd, "mcpServers": []any{}},
		}); err != nil {
			return fail(err)
		}
		// session/load replays the old conversation as session/update
		// notifications before it responds — forward them when the caller wants
		// the history rendered (picker resume).
		resp, err := reader.await(1, func(raw string, msg map[string]any) {
			if m, _ := msg["method"].(string); m == "session/update" && opts.EmitHistory {
				runtime.EventsEmit(a.ctx, "acp-data-"+opts.ID, raw)
			}
		})
		if err != nil {
			return fail(err)
		}
		if _, bad := resp["error"]; !bad {
			sessionID = sid
			result = mapOf(resp["result"])
		}
		// else: stale session id — fall through to session/new
	}
	if sessionID == "" {
		if err := sess.write(map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "session/new",
			"params": map[string]any{"cwd": opts.Cwd, "mcpServers": []any{}},
		}); err != nil {
			return fail(err)
		}
		resp, err := reader.await(2, nil)
		if err != nil {
			return fail(err)
		}
		result = mapOf(resp["result"])
		sid, _ := result["sessionId"].(string)
		if sid == "" {
			return fail(fmt.Errorf("session/new response missing sessionId"))
		}
		sessionID = sid
	}
	close(done)

	sess.sessionID = sessionID
	a.acpReg().put(opts.ID, sess)
	a.emitSession(opts.ID, sessionID, result["modes"], result["configOptions"])
	go a.pump(opts.ID, reader, sess)
	return nil
}

// CodexStart runs Codex through its native app-server (not an ACP adapter), and
// asks it for the model catalog so the model picker has real options.
func (a *App) CodexStart(id, cwd string, env map[string]string, resumeSessionID string) error {
	if _, live := a.acpReg().get(id); live {
		return nil
	}
	bin := resolveAgentBin("codex", cwd)
	if bin == "" {
		return fmt.Errorf("codex binary not found (checked ~/.local/bin, homebrew, PATH)")
	}
	e := map[string]string{}
	for k, v := range env {
		e[k] = v
	}
	e["PATH"] = augmentedPath(cwd)

	cmd, stdin, reader, stderrTail, err := spawnStdio(bin, []string{"app-server"}, cwd, e)
	if err != nil {
		return fmt.Errorf("failed to spawn codex app-server: %w", err)
	}
	sess := &acpSession{cmd: cmd, stdin: stdin, proto: protoCodexAppServer, nextID: 100}
	fail := func(err error) error {
		_ = cmd.Process.Kill()
		if tail := stderrTail(); tail != "" {
			return fmt.Errorf("%w\n--- codex stderr ---\n%s", err, tail)
		}
		return err
	}

	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "burrow", "version": appVersion},
			"capabilities": map[string]any{"experimentalApi": true},
		},
	}); err != nil {
		return fail(err)
	}
	if _, err := reader.await(0, nil); err != nil {
		return fail(err)
	}

	// model/list is Codex's own model catalog — this is what makes the picker
	// show real Codex models instead of a bare "Default".
	// Codex's own catalog — this is what fills the model picker. A failure here
	// is not fatal: the chat still runs on the CLI's default model.
	entries, next, err := codexModelPages(sess, reader, 1)
	if err != nil {
		entries = nil
	}
	configOptions := codexConfigOptions(entries)

	startParams := map[string]any{"cwd": cwd, "approvalPolicy": "on-request", "sandbox": "workspace-write"}
	var resp map[string]any
	if tid := strings.TrimSpace(resumeSessionID); tid != "" {
		if err := sess.write(map[string]any{
			"jsonrpc": "2.0", "id": next, "method": "thread/resume",
			"params": map[string]any{"threadId": tid, "cwd": cwd, "approvalPolicy": "on-request", "sandbox": "workspace-write"},
		}); err != nil {
			return fail(err)
		}
		r, err := reader.await(next, nil)
		if err != nil {
			return fail(err)
		}
		next++
		if _, bad := r["error"]; bad {
			if err := sess.write(map[string]any{"jsonrpc": "2.0", "id": next, "method": "thread/start", "params": startParams}); err != nil {
				return fail(err)
			}
			if r, err = reader.await(next, nil); err != nil {
				return fail(err)
			}
			next++
		}
		resp = r
	} else {
		if err := sess.write(map[string]any{"jsonrpc": "2.0", "id": next, "method": "thread/start", "params": startParams}); err != nil {
			return fail(err)
		}
		r, err := reader.await(next, nil)
		if err != nil {
			return fail(err)
		}
		next++
		resp = r
	}
	sess.nextID = next + 100
	threadID, _ := mapOf(mapOf(resp["result"])["thread"])["id"].(string)
	if threadID == "" {
		return fail(fmt.Errorf("codex thread/start response missing thread id"))
	}

	sess.sessionID = threadID
	a.acpReg().put(id, sess)
	a.emitSession(id, threadID, nil, configOptions)
	go a.pump(id, reader, sess)
	return nil
}

// codexConfigOptions reshapes a model/list response into the ACP configOptions
// shape the frontend's model/effort selectors already understand. Verified
// against codex-cli 0.149.1: `supportedReasoningEfforts` is a list of
// {reasoningEffort, description} objects (older docs suggest bare strings, so
// both are accepted).
func codexConfigOptions(data []any) []any {
	if len(data) == 0 {
		return []any{}
	}
	models := []any{}
	current, currentEffort := "", ""
	efforts := []any{}
	for _, entry := range data {
		m := mapOf(entry)
		if hidden, _ := m["hidden"].(bool); hidden {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			id, _ = m["model"].(string)
		}
		if id == "" {
			continue
		}
		name, _ := m["displayName"].(string)
		if name == "" {
			name = id
		}
		desc, _ := m["description"].(string)
		models = append(models, map[string]any{"value": id, "name": name, "description": desc})

		isDefault, _ := m["isDefault"].(bool)
		if current == "" || isDefault {
			current = id
			// The chosen model also defines the effort choices we offer.
			currentEffort, _ = m["defaultReasoningEffort"].(string)
			efforts = codexEfforts(m["supportedReasoningEfforts"])
		}
	}
	opts := []any{map[string]any{"id": "model", "name": "Model", "currentValue": current, "options": models}}
	if len(efforts) > 0 {
		opts = append(opts, map[string]any{"id": "effort", "name": "Effort", "currentValue": currentEffort, "options": efforts})
	}
	return opts
}

func codexEfforts(v any) []any {
	list, _ := v.([]any)
	out := []any{}
	for _, e := range list {
		switch t := e.(type) {
		case string:
			out = append(out, map[string]any{"value": t, "name": t})
		case map[string]any:
			id, _ := t["reasoningEffort"].(string)
			if id == "" {
				continue
			}
			desc, _ := t["description"].(string)
			out = append(out, map[string]any{"value": id, "name": id, "description": desc})
		}
	}
	return out
}

// AcpSend prompts the agent and returns the JSON-RPC id, so the frontend can
// match the turn-done response.
func (a *App) AcpSend(id, text string, images []string) (int64, error) {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return 0, fmt.Errorf("acp adapter not running")
	}
	rpc := sess.rpcID()

	if sess.proto == protoCodexAppServer {
		input := []any{}
		if text != "" {
			input = append(input, map[string]any{"type": "text", "text": text})
		}
		for _, uri := range images {
			input = append(input, map[string]any{"type": "image", "url": uri})
		}
		sess.mu.Lock()
		sess.pendingTurn = rpc
		model, effort := sess.model, sess.effort
		sess.mu.Unlock()
		params := map[string]any{"threadId": sess.sessionID, "input": input}
		// turn/start overrides apply to this turn and subsequent ones, which is
		// how a Codex model/effort switch takes effect at all.
		if model != "" {
			params["model"] = model
		}
		if effort != "" {
			params["effort"] = effort
		}
		return rpc, sess.write(map[string]any{"jsonrpc": "2.0", "id": rpc, "method": "turn/start", "params": params})
	}

	prompt := []any{}
	if text != "" {
		prompt = append(prompt, map[string]any{"type": "text", "text": text})
	}
	for _, uri := range images {
		mime, data := "image/png", uri
		if rest, ok := strings.CutPrefix(uri, "data:"); ok {
			if m, d, ok := strings.Cut(rest, ";base64,"); ok {
				mime, data = m, d
			}
		}
		prompt = append(prompt, map[string]any{"type": "image", "mimeType": mime, "data": data})
	}
	return rpc, sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpc, "method": "session/prompt",
		"params": map[string]any{"sessionId": sess.sessionID, "prompt": prompt},
	})
}

func (a *App) CodexSend(id, text string, images []string) (int64, error) {
	return a.AcpSend(id, text, images)
}

// AcpSetMode switches the session permission mode.
func (a *App) AcpSetMode(id, modeID string) (int64, error) {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return 0, fmt.Errorf("acp adapter not running")
	}
	rpc := sess.rpcID()
	return rpc, sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpc, "method": "session/set_mode",
		"params": map[string]any{"sessionId": sess.sessionID, "modeId": modeID},
	})
}

// AcpSetConfig sets a session config option (model / effort). Codex has no
// per-session config call — its overrides ride along with the next turn/start,
// so they're stashed on the session and answered locally.
func (a *App) AcpSetConfig(id, configID, value string) (int64, error) {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return 0, fmt.Errorf("acp adapter not running")
	}
	rpc := sess.rpcID()
	if sess.proto == protoCodexAppServer {
		sess.mu.Lock()
		switch configID {
		case "model":
			sess.model = value
		case "effort":
			sess.effort = value
		}
		sess.mu.Unlock()
		return rpc, nil
	}
	return rpc, sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpc, "method": "session/set_config_option",
		"params": map[string]any{"sessionId": sess.sessionID, "configId": configID, "value": value},
	})
}

// AcpListSessions asks for prior sessions in cwd (history picker).
func (a *App) AcpListSessions(id, cwd string) (int64, error) {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return 0, fmt.Errorf("acp adapter not running")
	}
	rpc := sess.rpcID()
	if sess.proto == protoCodexAppServer {
		return rpc, sess.write(map[string]any{
			"jsonrpc": "2.0", "id": rpc, "method": "thread/list", "params": map[string]any{"cwd": cwd},
		})
	}
	return rpc, sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpc, "method": "session/list", "params": map[string]any{"cwd": cwd},
	})
}

func (a *App) AcpStop(id string) error {
	sess := a.acpReg().drop(id)
	if sess == nil {
		return nil
	}
	_ = sess.stdin.Close()
	if sess.cmd.Process != nil {
		return sess.cmd.Process.Kill()
	}
	return nil
}

func (a *App) CodexStop(id string) error {
	return a.AcpStop(id)
}

// --- Model discovery without a chat --------------------------------------
// The picker needs Codex's model catalog before any session exists (welcome
// screen). t3code solves this by probing: spawn `codex app-server`, initialize,
// page through `model/list`, kill it. Same here.

// AgentModel is one entry for the frontend's model picker.
type AgentModel struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// CodexListModels probes the locally installed Codex for its model catalog.
func (a *App) CodexListModels(cwd string) ([]AgentModel, error) {
	bin := resolveAgentBin("codex", cwd)
	if bin == "" {
		return nil, fmt.Errorf("codex binary not found (checked ~/.local/bin, homebrew, PATH)")
	}
	cmd, stdin, reader, stderrTail, err := spawnStdio(bin, []string{"app-server"}, cwd, map[string]string{"PATH": augmentedPath(cwd)})
	if err != nil {
		return nil, fmt.Errorf("failed to spawn codex app-server: %w", err)
	}
	// Killing the child is also how a blocked read unblocks.
	stop := time.AfterFunc(20*time.Second, func() { _ = cmd.Process.Kill() })
	defer func() {
		stop.Stop()
		_ = stdin.Close()
		_ = cmd.Process.Kill()
	}()

	sess := &acpSession{stdin: stdin, proto: protoCodexAppServer}
	fail := func(err error) ([]AgentModel, error) {
		if tail := stderrTail(); tail != "" {
			return nil, fmt.Errorf("%w\n--- codex stderr ---\n%s", err, tail)
		}
		return nil, err
	}
	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "burrow", "title": "Burrow", "version": appVersion},
			"capabilities": map[string]any{"experimentalApi": true},
		},
	}); err != nil {
		return fail(err)
	}
	if _, err := reader.await(0, nil); err != nil {
		return fail(err)
	}

	entries, _, err := codexModelPages(sess, reader, 1)
	if err != nil {
		return fail(err)
	}
	return codexModels(entries), nil
}

// codexModelPages walks `model/list` to the end of its cursor pagination and
// returns the raw model entries plus the next free rpc id.
func codexModelPages(sess *acpSession, reader *jsonRPCReader, startID int64) ([]any, int64, error) {
	entries := []any{}
	cursor := ""
	rpc := startID
	for ; ; rpc++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := sess.write(map[string]any{"jsonrpc": "2.0", "id": rpc, "method": "model/list", "params": params}); err != nil {
			return entries, rpc, err
		}
		resp, err := reader.await(rpc, nil)
		if err != nil {
			return entries, rpc, err
		}
		result := mapOf(resp["result"])
		if page, ok := result["data"].([]any); ok {
			entries = append(entries, page...)
		}
		next, _ := result["nextCursor"].(string)
		if next == "" || next == cursor {
			break
		}
		cursor = next
	}
	return entries, rpc + 1, nil
}

// codexModels flattens model/list entries, dropping picker-hidden ones.
func codexModels(data []any) []AgentModel {
	out := []AgentModel{}
	for _, entry := range data {
		m := mapOf(entry)
		if hidden, _ := m["hidden"].(bool); hidden {
			continue
		}
		id, _ := m["model"].(string)
		if id == "" {
			id, _ = m["id"].(string)
		}
		if id == "" {
			continue
		}
		label, _ := m["displayName"].(string)
		if label == "" {
			label = id
		}
		desc, _ := m["description"].(string)
		out = append(out, AgentModel{ID: id, Label: label, Description: desc})
	}
	return out
}
