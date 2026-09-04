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

	mu           sync.Mutex
	nextID       int64
	pendingTurn  int64 // Codex: rpc id of the turn awaiting turn/completed (0 = none)
	turnWatchdog *time.Timer
	model        string // Codex: model override applied on the next turn/start
	effort       string
}

const codexTurnSilenceTimeout = 10 * time.Minute

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

// dropIf forgets the session only while it is still the live one for that id, so
// a reader goroutine finishing after a restart cannot evict its replacement.
func (r *acpRegistry) dropIf(id string, s *acpSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[id] == s {
		delete(r.live, id)
	}
}

func (r *acpRegistry) drop(id string) *acpSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.live[id]
	delete(r.live, id)
	return s
}

// ids snapshots the live session ids (shutdown cleanup iterates these).
func (r *acpRegistry) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.live))
	for id := range r.live {
		out = append(out, id)
	}
	return out
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
	EmitHistory     bool              `json:"emitHistory"`
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
		kind := "acp-data"
		if hasMethod && hasID {
			kind = "acp-req"
		}
		a.emitChatLine(chatID, kind, raw)
	}
	// The child is gone: forget it BEFORE announcing the exit. AcpStart and
	// CodexStart both short-circuit on "a session for this id is already live",
	// so a dead session left in the registry made the next send write into a
	// closed stdin (`write |1: broken pipe`) instead of spawning a replacement.
	a.acpReg().dropIf(chatID, sess)
	a.emitChatLine(chatID, "acp-data", `{"_burrow":"exit"}`)
}

// pumpCodexLine translates Codex app-server notifications into the ACP
// session/update shape the frontend already renders.
func (a *App) pumpCodexLine(chatID string, msg map[string]any, sess *acpSession) {
	// Any app-server event proves the child and its reader are alive. Reset the
	// watchdog before translating it; a completely silent live child is the
	// failure mode that used to leave the composer thinking indefinitely.
	a.resetCodexTurnWatchdog(chatID, sess)
	method, _ := msg["method"].(string)
	params := mapOf(msg["params"])
	emit := func(v any) {
		line, err := json.Marshal(v)
		if err != nil {
			return
		}
		a.emitChatLine(chatID, "acp-data", string(line))
	}
	// `turn/start` is acknowledged with an ordinary JSON-RPC response.  A
	// rejection therefore has no `method`, and used to be silently discarded by
	// this bridge.  The UI had already set busy=true, so it then spun until the
	// silence watchdog fired.  Only an error settles a pending turn here: a
	// successful response merely means the turn is now running and its terminal
	// notification remains authoritative.
	if method == "" {
		if failure := codexRPCErrorMessage(msg); failure != "" {
			a.finishCodexTurn(chatID, sess, emit, failure)
		}
		return
	}
	switch method {
	case "item/started":
		if toolCallID, title, input, ok := codexToolCall(mapOf(params["item"])); ok {
			update := map[string]any{"sessionUpdate": "tool_call", "toolCallId": toolCallID, "title": title, "input": input}
			emit(map[string]any{"method": "session/update", "params": map[string]any{"update": update}})
		}
	case "item/completed":
		if toolCallID, _, _, ok := codexToolCall(mapOf(params["item"])); ok {
			item := mapOf(params["item"])
			failed := codexToolFailed(item)
			status := "completed"
			if failed {
				status = "failed"
			}
			update := map[string]any{
				"sessionUpdate": "tool_call_update", "toolCallId": toolCallID, "status": status,
				"content": []any{map[string]any{"content": map[string]any{"type": "text", "text": codexToolOutput(item)}}},
			}
			emit(map[string]any{"method": "session/update", "params": map[string]any{"update": update}})
		}
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
		a.finishCodexTurn(chatID, sess, emit, codexTurnTerminalFailure(params))
	case "turn/aborted":
		// Newer app-server versions may report an aborted turn separately instead
		// of (or before) turn/completed.  Leaving pendingTurn set in that case is
		// precisely what made the composer spin forever.
		a.finishCodexTurn(chatID, sess, emit, "Codex aborted the turn.")
	case "error":
		// `error` is terminal unless Codex says it will retry.  T3code treats the
		// same notification as an error state; settle our synthetic prompt reply
		// too, so a runtime failure cannot strand the chat in Thinking.
		willRetry, _ := params["willRetry"].(bool)
		if !willRetry {
			a.finishCodexTurn(chatID, sess, emit, codexErrorMessage(params))
		}
	case "serverRequest/resolved":
		// This is Codex's authoritative acknowledgement that an approval (or
		// input request) is no longer pending. Forward it so the UI does not
		// optimistically clear the prompt before the app-server accepted it.
		emit(map[string]any{"method": method, "params": params})
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		// Blocking approval requests. `parseAcpPermRequest` understands exactly
		// these three method names, so they belong on the request channel — the
		// default branch below was rejecting every Codex approval as unsupported,
		// which is why a Supervised turn died with "the environment rejected the
		// command approval request".
		if line, err := json.Marshal(msg); err == nil {
			a.emitChatLine(chatID, "acp-req", string(line))
		}
	case "item/tool/requestUserInput":
		// Unlike an approval this request has a structured answers response.  Keep
		// it on the request channel so the dedicated Codex input panel can respond.
		line, err := json.Marshal(msg)
		if err == nil {
			a.emitChatLine(chatID, "acp-req", string(line))
		}
	default:
		if _, hasID := msg["id"]; hasID {
			// A JSON-RPC server request is blocking.  Previously we forwarded every
			// unknown request to the ACP permission UI, which only understands
			// approvals.  Requests such as item/tool/call and requestUserInput were
			// then ignored by the UI and Codex waited for a response forever.
			// Explicitly reject unsupported request types so the turn can fail with a
			// useful error instead of stranding an agent on a tool call.  The known
			// approval requests still travel through acp-req above this default.
			a.rejectUnsupportedCodexRequest(sess, msg, method)
		}
	}
}

// rejectUnsupportedCodexRequest completes a server-initiated JSON-RPC request
// that Burrow cannot safely fulfill yet.  A response is mandatory: omitting it
// leaves the Codex app-server blocked indefinitely.
func (a *App) rejectUnsupportedCodexRequest(sess *acpSession, msg map[string]any, method string) {
	if err := sess.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      msg["id"],
		"error": map[string]any{
			"code":    -32601,
			"message": "Burrow does not support the blocking Codex request " + method,
		},
	}); err != nil {
		return
	}
}

// codexToolCall turns the app-server's heterogeneous thread items into the
// compact tool cards the ACP chat renderer already understands.
func codexToolCall(item map[string]any) (id, title string, input map[string]any, ok bool) {
	id, _ = item["id"].(string)
	if id == "" {
		return "", "", nil, false
	}
	switch item["type"] {
	case "commandExecution":
		command, _ := item["command"].(string)
		return id, "Run: " + command, map[string]any{"command": command, "cwd": item["cwd"]}, true
	case "fileChange":
		return id, "Apply file changes", map[string]any{"changes": item["changes"]}, true
	case "mcpToolCall":
		server, _ := item["server"].(string)
		tool, _ := item["tool"].(string)
		return id, server + ": " + tool, map[string]any{"arguments": item["arguments"]}, true
	case "dynamicToolCall":
		tool, _ := item["tool"].(string)
		return id, tool, map[string]any{"arguments": item["arguments"]}, true
	case "webSearch":
		query, _ := item["query"].(string)
		return id, "Web search", map[string]any{"query": query}, true
	default:
		return "", "", nil, false
	}
}

func codexToolFailed(item map[string]any) bool {
	status, _ := item["status"].(string)
	if status == "failed" || status == "error" {
		return true
	}
	return mapOf(item["error"])["message"] != nil
}

func codexToolOutput(item map[string]any) string {
	for _, key := range []string{"aggregatedOutput", "result"} {
		if text, ok := item[key].(string); ok && text != "" {
			return text
		}
	}
	if err := mapOf(item["error"]); err != nil {
		if text, _ := err["message"].(string); text != "" {
			return text
		}
	}
	if out, err := json.Marshal(item); err == nil {
		return string(out)
	}
	return ""
}

func codexRPCErrorMessage(msg map[string]any) string {
	err := mapOf(msg["error"])
	if err == nil {
		return ""
	}
	if message, _ := err["message"].(string); message != "" {
		return message
	}
	return "The Codex app-server rejected the request without an error message."
}

// finishCodexTurn turns a terminal Codex notification into the prompt response
// consumed by AgentChat.  Codex completes turns by notification, unlike ACP's
// request/response session/prompt API.
func (a *App) finishCodexTurn(chatID string, sess *acpSession, emit func(any), failure string) {
	sess.mu.Lock()
	rpc := sess.pendingTurn
	sess.pendingTurn = 0
	if sess.turnWatchdog != nil {
		sess.turnWatchdog.Stop()
		sess.turnWatchdog = nil
	}
	sess.mu.Unlock()
	if rpc == 0 {
		return
	}
	if failure != "" {
		emit(map[string]any{"method": "session/update", "params": map[string]any{
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"messageId":     "codex-runtime-error",
				"content":       map[string]any{"text": "Codex error: " + failure},
			}}})
	}
	emit(map[string]any{"id": rpc, "result": map[string]any{}})
}

// resetCodexTurnWatchdog settles only a truly silent live app-server. Normal
// tool-heavy turns keep emitting progress and therefore keep the timer fresh.
func (a *App) resetCodexTurnWatchdog(chatID string, sess *acpSession) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.pendingTurn == 0 {
		return
	}
	if sess.turnWatchdog != nil {
		sess.turnWatchdog.Stop()
	}
	sess.turnWatchdog = time.AfterFunc(codexTurnSilenceTimeout, func() {
		a.finishCodexTurn(chatID, sess, func(v any) {
			line, err := json.Marshal(v)
			if err == nil {
				a.emitChatLine(chatID, "acp-data", string(line))
			}
		}, "The Codex app-server produced no events for 10 minutes. Stop and retry the turn.")
	})
}

// codexTurnTerminalFailure extracts a useful message from turn/completed.  A
// completed or interrupted turn is still terminal, but only a failed one needs
// an error bubble in the chat.
func codexTurnTerminalFailure(params map[string]any) string {
	turn := mapOf(params["turn"])
	if status, _ := turn["status"].(string); status == "failed" {
		return codexErrorMessage(turn)
	}
	return ""
}

func codexErrorMessage(params map[string]any) string {
	err := mapOf(params["error"])
	if message, _ := err["message"].(string); message != "" {
		return message
	}
	return "The Codex app-server ended the turn without an error message."
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
	a.emitChatLine(chatID, "acp-data", string(line))
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
	a.addBurrowEnv(env, opts.Cwd)

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
			"params": map[string]any{"sessionId": sid, "cwd": opts.Cwd, "mcpServers": acpMcpServers()},
		}); err != nil {
			return fail(err)
		}
		// session/load replays the old conversation as session/update
		// notifications before it responds — forward them when the caller wants
		// the history rendered (picker resume).
		resp, err := reader.await(1, func(raw string, msg map[string]any) {
			if m, _ := msg["method"].(string); m == "session/update" && opts.EmitHistory {
				a.emitChatLine(opts.ID, "acp-data", raw)
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
			"params": map[string]any{"cwd": opts.Cwd, "mcpServers": acpMcpServers()},
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
	a.addBurrowEnv(e, cwd)

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

	// A fresh Codex thread starts in T3-style Auto: routine requests go through
	// Codex's own risk-based reviewer, while risky ones still surface to the user.
	// Other picker choices replace this with their explicit reviewer on update.
	startParams := map[string]any{
		"cwd": cwd, "approvalPolicy": "on-request", "approvalsReviewer": "auto_review", "sandbox": "workspace-write",
	}
	var resp map[string]any
	if tid := strings.TrimSpace(resumeSessionID); tid != "" {
		if err := sess.write(map[string]any{
			"jsonrpc": "2.0", "id": next, "method": "thread/resume",
			"params": map[string]any{
				"threadId": tid, "cwd": cwd, "approvalPolicy": "on-request", "approvalsReviewer": "auto_review", "sandbox": "workspace-write",
			},
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
	a.emitSession(id, threadID, codexModes(), configOptions)
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

// effortIDs is codexEfforts reduced to bare ids, for the model catalog.
func effortIDs(v any) []string {
	out := []string{}
	for _, e := range codexEfforts(v) {
		if id, _ := mapOf(e)["value"].(string); id != "" {
			out = append(out, id)
		}
	}
	return out
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
		a.resetCodexTurnWatchdog(id, sess)
		params := map[string]any{"threadId": sess.sessionID, "input": input}
		// turn/start overrides apply to this turn and subsequent ones, which is
		// how a Codex model/effort switch takes effect at all.
		if model != "" {
			params["model"] = model
		}
		if effort != "" {
			params["effort"] = effort
		}
		err := sess.write(map[string]any{"jsonrpc": "2.0", "id": rpc, "method": "turn/start", "params": params})
		if err != nil {
			a.finishCodexTurn(id, sess, func(any) {}, "")
		}
		return rpc, err
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
	if sess.proto == protoCodexAppServer {
		approvalPolicy, sandbox, reviewer, ok := codexModeSettings(modeID)
		if !ok {
			return 0, fmt.Errorf("unsupported Codex permission mode %q", modeID)
		}
		// Codex is not an ACP session.  Sending session/set_mode here used an
		// incompatible session id and produced "unknown agent session".  Its
		// v2 app-server API updates an existing thread in place, so the next turn
		// keeps its history and adopts the new permission settings.
		return rpc, sess.write(map[string]any{
			"jsonrpc": "2.0", "id": rpc, "method": "thread/settings/update",
			"params": map[string]any{
				"threadId": sess.sessionID, "approvalPolicy": approvalPolicy,
				"approvalsReviewer": reviewer, "sandboxPolicy": map[string]any{"type": sandbox},
			},
		})
	}
	return rpc, sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpc, "method": "session/set_mode",
		"params": map[string]any{"sessionId": sess.sessionID, "modeId": modeID},
	})
}

// codexModes is the permission-mode catalogue for a Codex chat, in the shape
// the frontend's mode switcher already reads from ACP adapters. Codex has no
// call that lists its own modes, so it is spelled out here — the ids must stay
// in sync with codexModeSettings, which translates them for the app-server.
// currentModeId matches the approvalPolicy/sandbox in CodexStart's startParams.
func codexModes() map[string]any {
	return map[string]any{
		"currentModeId": "auto",
		"availableModes": []map[string]any{
			{"id": "read-only", "name": "Supervised", "description": "Ask before commands and file changes."},
			{"id": "auto-accept-edits", "name": "Auto-accept edits", "description": "Auto-approve edits, ask before other actions."},
			{"id": "auto", "name": "Auto", "description": "Codex reviews routine actions automatically; risky actions still ask."},
			{"id": "dontAsk", "name": "Don't ask", "description": "No approval prompts, still confined to the workspace."},
			{"id": "full-access", "name": "Full access", "description": "Allow commands and edits without prompts."},
		},
	}
}

// codexModeSettings translates Burrow's shared mode ids to the settings the
// Codex app-server accepts.  Keep the aliases: ACP agents may expose the
// descriptive Codex names while the legacy Claude dropdown uses its own ids.
func codexModeSettings(modeID string) (approvalPolicy, sandbox, reviewer string, ok bool) {
	switch modeID {
	case "default", "ask", "approval-required", "plan", "read-only":
		return "untrusted", "readOnly", "user", true
	case "acceptEdits", "auto-accept-edits":
		return "on-request", "workspaceWrite", "user", true
	case "auto", "auto-review":
		return "on-request", "workspaceWrite", "auto_review", true
	case "dontAsk":
		return "never", "workspaceWrite", "user", true
	case "bypassPermissions", "full-access", "danger-full-access":
		return "never", "dangerFullAccess", "user", true
	default:
		return "", "", "", false
	}
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
	sess.mu.Lock()
	if sess.turnWatchdog != nil {
		sess.turnWatchdog.Stop()
		sess.turnWatchdog = nil
	}
	sess.mu.Unlock()
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
	// Reasoning efforts this model accepts. Codex reports them per model, so the
	// composer can offer the right set before any session exists.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
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
		def, _ := m["defaultReasoningEffort"].(string)
		out = append(out, AgentModel{
			ID: id, Label: label, Description: desc,
			Efforts:       effortIDs(m["supportedReasoningEfforts"]),
			DefaultEffort: def,
		})
	}
	return out
}
