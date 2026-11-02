package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProcs is the test adapter of chatProcs: no processes, just a record of
// what the runtime asked for. ctrlReply ("success"/"error"/"") is how it
// answers a control_request ("" = never answers).
type fakeProcs struct {
	mu        sync.Mutex
	alive     map[string]bool
	starts    []fakeStart
	sent      []string
	onLine    map[string]func(string)
	ctrlReply string
	rpc       *fakeRPC // the Codex / ACP half (App.rpcStarter)
}

// fakeRPC registers an acpSession over a buffer instead of spawning an
// app-server / adapter, so what the runtime writes to it can be read back.
type fakeRPC struct {
	mu         sync.Mutex
	starts     []AcpStartOpts
	transports []string
	writers    map[string]*bytes.Buffer
}

func (f *fakeRPC) starter(a *App) func(AcpStartOpts, string) error {
	return func(o AcpStartOpts, transport string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		buf := &bytes.Buffer{}
		f.writers[o.ID] = buf
		f.starts = append(f.starts, o)
		f.transports = append(f.transports, transport)
		proto := protoACP
		if transport == "codex-app-server" {
			proto = protoCodexAppServer
		}
		a.acpReg().put(o.ID, &acpSession{stdin: nopWriteCloser{Writer: buf}, proto: proto, sessionID: "thread-1", nextID: 100})
		return nil
	}
}

// methods lists the JSON-RPC methods written to chat id's session, in order.
func (f *fakeRPC) methods(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(f.writers[id].String()), "\n") {
		var m struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m.Method)
		}
	}
	return out
}

func (f *fakeRPC) written(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writers[id].String()
}

type fakeStart struct {
	id, cmd, cwd string
	args         []string
	env          []string
}

func newFakeProcs() *fakeProcs {
	return &fakeProcs{alive: map[string]bool{}, onLine: map[string]func(string){}}
}

func (f *fakeProcs) Start(id, command string, args []string, cwd string, env []string, onLine func(string), onExit func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[id] = true
	f.onLine[id] = onLine
	f.starts = append(f.starts, fakeStart{id: id, cmd: command, cwd: cwd, args: args, env: env})
	return nil
}

func (f *fakeProcs) Send(id, data string) error {
	f.mu.Lock()
	if !f.alive[id] {
		f.mu.Unlock()
		return os.ErrProcessDone
	}
	f.sent = append(f.sent, data)
	reply, onLine := f.ctrlReply, f.onLine[id]
	f.mu.Unlock()
	var req struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal([]byte(data), &req) == nil && req.Type == "control_request" && reply != "" {
		go onLine(`{"type":"control_response","response":{"subtype":"` + reply + `","request_id":"` + req.RequestID + `"}}`)
	}
	return nil
}

func (f *fakeProcs) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.alive, id)
	return nil
}

func (f *fakeProcs) Signal(id string, _ os.Signal) error { return nil }

func (f *fakeProcs) Alive(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[id]
}

func (f *fakeProcs) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func (f *fakeProcs) lastStart() fakeStart {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts[len(f.starts)-1]
}

// runtimeApp is an App with a DB, the fake process starter, a temp HOME (for
// config.json) and one workspace. writeConfig sets config.json.
func runtimeApp(t *testing.T) (*App, *fakeProcs, int64, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BURROW_DATA_SUFFIX", "")
	a := newTestApp(t)
	// Runs before newTestApp's db.Close (cleanups are LIFO): the async stream
	// writer must not write into a closed DB / removed TempDir.
	t.Cleanup(func() { drainChatStream(t, a, "1") })
	fake := newFakeProcs()
	a.claudeProcs = fake
	fake.rpc = &fakeRPC{writers: map[string]*bytes.Buffer{}}
	a.rpcStarter = fake.rpc.starter(a)
	ws := t.TempDir()
	res, err := a.db.Exec(`INSERT INTO workspaces (name, path, created_at) VALUES ('w', ?, 0)`, ws)
	if err != nil {
		t.Fatal(err)
	}
	wsID, _ := res.LastInsertId()
	return a, fake, wsID, ws
}

func writeConfig(t *testing.T, cfg map[string]any) {
	t.Helper()
	path, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// claudeAt is a provider instance whose binary exists, so resolveAgentBin
// accepts it without a real `claude` installed.
func claudeAt(id, configDir string) map[string]any {
	return map[string]any{"id": id, "name": id, "providerId": "claude", "kind": "claude", "transport": "claude-cli", "enabled": true, "binary": "/bin/sh", "args": []string{"--x"}, "configDir": configDir}
}

func hasArgPair(args []string, k, v string) bool {
	i := slices.Index(args, k)
	return i >= 0 && i+1 < len(args) && args[i+1] == v
}

func TestChatSendStartsLazilyWithRowSettings(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", ""), claudeAt("work", "/cfg/work")}})
	c, err := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1",
		Model: "claude-opus-x", Effort: "max", PermissionMode: "plan", ProfileID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.startCount() != 0 {
		t.Fatal("creating a chat must not start a process")
	}
	if err := a.ChatSend(c.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(c.ID, "again", nil); err != nil {
		t.Fatal(err)
	}
	if fake.startCount() != 1 {
		t.Fatalf("want one start for two sends, got %d", fake.startCount())
	}
	s := fake.lastStart()
	if s.cmd != "/bin/sh" || !hasArgPair(s.args, "--resume", "sess-1") || !hasArgPair(s.args, "--model", "claude-opus-x") ||
		!hasArgPair(s.args, "--effort", "max") || !hasArgPair(s.args, "--permission-mode", "default") || !slices.Contains(s.args, "--x") {
		t.Fatalf("start did not use the row + profile: %s %v", s.cmd, s.args)
	}
	if !slices.Contains(s.env, "CLAUDE_CONFIG_DIR=/cfg/work") {
		t.Fatalf("profile config dir missing: %v", s.env)
	}
	if len(fake.sent) != 2 || !strings.Contains(fake.sent[0], `"hello"`) || !strings.Contains(fake.sent[0], `"session_id":"sess-1"`) {
		t.Fatalf("sends: %v", fake.sent)
	}
}

func TestChatSendFallsBackToProjectConfigDir(t *testing.T) {
	a, fake, wsID, ws := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	if err := os.MkdirAll(filepath.Join(ws, ".burrow"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".burrow", "config.toml"), []byte("[settings]\nclaude_config_dir = \"/cfg/project\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if err := a.ChatSend(c.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if s := fake.lastStart(); !slices.Contains(s.env, "CLAUDE_CONFIG_DIR=/cfg/project") || slices.Contains(s.args, "--resume") {
		t.Fatalf("want project config dir and no resume: %v %v", s.env, s.args)
	}
}

func TestChatSendStartsAgainAfterReap(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if err := a.ChatSend(c.ID, "one", nil); err != nil {
		t.Fatal(err)
	}
	// The CLI reports its session; the runtime records it on the row.
	fake.onLine["1"](`{"type":"system","subtype":"init","session_id":"sess-9"}`)
	_ = fake.Stop("1") // auto-sleep
	if err := a.ChatSend(c.ID, "two", nil); err != nil {
		t.Fatal(err)
	}
	if fake.startCount() != 2 || !hasArgPair(fake.lastStart().args, "--resume", "sess-9") {
		t.Fatalf("want a resumed second start, got %d starts %v", fake.startCount(), fake.lastStart().args)
	}
}

func TestChatSendStartsCodexFromRow(t *testing.T) {
	a, fake, wsID, ws := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{map[string]any{
		"id": "codex-work", "name": "Codex", "providerId": "codex", "kind": "codex", "transport": "codex-app-server",
		"enabled": true, "env": map[string]string{"CODEX_HOME": "/cx"}, "transportArgs": []string{"app-server"},
	}}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex-work",
		ClaudeSessionID: "thr-0", Model: "gpt-x", Effort: "high", PermissionMode: "plan"})
	if err := a.ChatSend(c.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(c.ID, "again", nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.rpc.starts) != 1 || fake.rpc.transports[0] != "codex-app-server" {
		t.Fatalf("want one codex start, got %v", fake.rpc.transports)
	}
	o := fake.rpc.starts[0]
	if o.ResumeSessionID != "thr-0" || o.Cwd != ws || o.Env["CODEX_HOME"] != "/cx" {
		t.Fatalf("start did not use the row + provider: %+v", o)
	}
	got := fake.rpc.methods("1")
	if !slices.Equal(got, []string{"thread/settings/update", "turn/start", "turn/start"}) {
		t.Fatalf("writes: %v", got)
	}
	if w := fake.rpc.written("1"); !strings.Contains(w, `"model":"gpt-x"`) || !strings.Contains(w, `"effort":"high"`) || !strings.Contains(w, `"readOnly"`) {
		t.Fatalf("row settings not applied: %s", w)
	}
}

func TestChatSendStartsAcpFromRow(t *testing.T) {
	a, fake, wsID, ws := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{map[string]any{
		"id": "gem", "name": "Gemini", "providerId": "gemini", "kind": "gemini", "transport": "acp",
		"enabled": true, "transportArgs": []string{"--acp"},
	}}})
	if err := os.MkdirAll(filepath.Join(ws, ".burrow"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ws, ".burrow", "config.toml"), []byte("[settings]\nenv_file = \".env.local\"\nclaude_config_dir = \"/cfg\"\n"), 0o644)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "acp", AgentKind: "gem", Model: "m1"})
	if err := a.ChatSend(c.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	o := fake.rpc.starts[0]
	if o.Command != "gemini" || !slices.Equal(o.Args, []string{"--acp"}) || o.Kind != "gemini" || o.EnvFile != ".env.local" || o.ConfigDir != "/cfg" {
		t.Fatalf("acp start payload: %+v", o)
	}
	if got := fake.rpc.methods("1"); !slices.Equal(got, []string{"session/set_config_option", "session/prompt"}) {
		t.Fatalf("writes: %v", got)
	}
}

func TestChatSendRefusesUnknownAcpProvider(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "acp", AgentKind: "nope"})
	if err := a.ChatSend(c.ID, "hi", nil); err == nil || len(fake.rpc.starts) != 0 {
		t.Fatalf("want an error and no start, err=%v starts=%d", err, len(fake.rpc.starts))
	}
}

func TestCodexPermissionModeFailsClosed(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex", PermissionMode: "bogus"})
	if err := a.ChatSend(c.ID, "hi", nil); err == nil {
		t.Fatal("an unappliable mode must refuse the send")
	}
	if _, live := a.acpReg().get("1"); live {
		t.Fatal("the session that could not take the mode must be stopped")
	}
}

func TestRPCRestartClearAndInterrupt(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex", ClaudeSessionID: "thr-0"})
	events := chatEvents(t, "1")
	live := func() bool { _, ok := a.acpReg().get("1"); return ok }

	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatRestart(c.ID); err != nil || live() {
		t.Fatalf("restart must stop the session: err=%v live=%v", err, live())
	}
	if got := events(); got[len(got)-1].Type != EvtSessionReset {
		t.Fatalf("want a reset, got %+v", got)
	}

	// No addressable turn: the interrupt falls back to a restart.
	_ = a.ChatSend(c.ID, "hi", nil)
	sess, _ := a.acpReg().get("1")
	sess.mu.Lock()
	sess.turnID = ""
	sess.mu.Unlock()
	if _ = a.ChatInterrupt(c.ID); live() {
		t.Fatal("an unaddressable interrupt must restart")
	}

	// Accepted, but the turn never ends: restarted after the grace.
	old := codexInterruptGrace
	codexInterruptGrace = 10 * time.Millisecond
	t.Cleanup(func() { codexInterruptGrace = old })
	_ = a.ChatSend(c.ID, "hi", nil)
	sess, _ = a.acpReg().get("1")
	sess.mu.Lock()
	sess.turnID = "turn-1"
	sess.mu.Unlock()
	_ = a.ChatInterrupt(c.ID)
	if !live() || !strings.Contains(fake.rpc.written("1"), `"turn/interrupt"`) {
		t.Fatal("an accepted interrupt keeps the session")
	}
	for i := 0; i < 200 && live(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if live() {
		t.Fatal("a turn still running after the grace must be restarted")
	}

	// Clear forgets the thread: the next start does not resume it.
	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatClear(c.ID); err != nil || live() {
		t.Fatalf("clear: err=%v live=%v", err, live())
	}
	_ = a.ChatSend(c.ID, "fresh", nil)
	if o := fake.rpc.starts[len(fake.rpc.starts)-1]; o.ResumeSessionID != "" {
		t.Fatalf("cleared chat resumed %q", o.ResumeSessionID)
	}
}

func TestRPCLaunchSettingsApplyLive(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex"})
	str := func(s string) *string { return &s }
	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{Model: str("gpt-y"), Effort: str("low"), PermissionMode: str("auto")}); err != nil {
		t.Fatal(err)
	}
	sess, live := a.acpReg().get("1")
	if !live || sess.model != "gpt-y" || sess.effort != "low" {
		t.Fatalf("live apply: live=%v %+v", live, sess)
	}
	if row, _ := a.chatByID(c.ID); row.Model != "gpt-y" || row.PermissionMode != "auto" {
		t.Fatalf("row not written: %+v", row)
	}
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{ProfileID: str("other")}); err != nil {
		t.Fatal(err)
	}
	if _, live := a.acpReg().get("1"); live {
		t.Fatal("a profile change must restart")
	}
}

// chatEvents collects session.reset events published for chat id.
func chatEvents(t *testing.T, id string) func() []ProviderRuntimeEvent {
	var mu sync.Mutex
	var got []ProviderRuntimeEvent
	unsub := busSubscribe(func(ev shellEvent) {
		if ev.Name != "chat-event-"+id {
			return
		}
		if b, ok := ev.Payload.(ChatEventBatch); ok {
			mu.Lock()
			got = append(got, b.Events...)
			mu.Unlock()
		}
	})
	t.Cleanup(unsub)
	return func() []ProviderRuntimeEvent {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func TestChatRestartStopsAndEmitsReset(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	events := chatEvents(t, "1")
	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatRestart(c.ID); err != nil {
		t.Fatal(err)
	}
	if fake.Alive("1") {
		t.Fatal("restart must stop the process")
	}
	got := events()
	if len(got) == 0 || got[len(got)-1].Type != EvtSessionReset || got[len(got)-1].Cleared {
		t.Fatalf("want a trailing session.reset, got %+v", got)
	}
}

func TestChatClearWipesTranscriptAndSession(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1", MessageCount: 4})
	if err := a.SaveChatMessages(int(c.ID), `[{"id":1,"role":"user","text":"old"}]`, -1); err != nil {
		t.Fatal(err)
	}
	events := chatEvents(t, "1")
	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatClear(c.ID); err != nil {
		t.Fatal(err)
	}
	row, _ := a.chatByID(c.ID)
	if row.ClaudeSessionID != "" || row.MessageCount != 0 || fake.Alive("1") {
		t.Fatalf("clear left state behind: %+v alive=%v", row, fake.Alive("1"))
	}
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE chat_id = ?`, c.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("chat_messages not wiped: %d rows", n)
	}
	got := events()
	if len(got) == 0 || got[len(got)-1].Type != EvtSessionReset || !got[len(got)-1].Cleared {
		t.Fatalf("want session.reset{cleared}, got %+v", got)
	}
	// The next send starts fresh — no resume of the forgotten session.
	_ = a.ChatSend(c.ID, "new", nil)
	if slices.Contains(fake.lastStart().args, "--resume") {
		t.Fatalf("cleared chat resumed: %v", fake.lastStart().args)
	}
}

func TestChatSetLaunchSettings(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	str := func(s string) *string { return &s }

	// Cold chat: only the row changes.
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{Effort: str("low")}); err != nil {
		t.Fatal(err)
	}
	if row, _ := a.chatByID(c.ID); row.Effort != "low" || fake.startCount() != 0 {
		t.Fatalf("cold patch: %+v starts=%d", row, fake.startCount())
	}

	// Live model change, confirmed by the CLI: process stays.
	fake.ctrlReply = "success"
	_ = a.ChatSend(c.ID, "hi", nil)
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{Model: str("claude-sonnet-x")}); err != nil {
		t.Fatal(err)
	}
	if !fake.Alive("1") || !strings.Contains(fake.sent[len(fake.sent)-1], `"set_model"`) {
		t.Fatalf("model should apply live: alive=%v sent=%v", fake.Alive("1"), fake.sent)
	}

	// Refused permission change: falls back to a restart.
	fake.ctrlReply = "error"
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{PermissionMode: str("plan")}); err != nil {
		t.Fatal(err)
	}
	if fake.Alive("1") {
		t.Fatal("a refused live change must restart")
	}

	// Effort has no live control: restart, and the next start uses it.
	_ = a.ChatSend(c.ID, "again", nil)
	if err := a.ChatSetLaunchSettings(c.ID, LaunchSettingsPatch{Effort: str("high")}); err != nil {
		t.Fatal(err)
	}
	_ = a.ChatSend(c.ID, "third", nil)
	s := fake.lastStart()
	if !hasArgPair(s.args, "--effort", "high") || !hasArgPair(s.args, "--model", "claude-sonnet-x") || !hasArgPair(s.args, "--permission-mode", "default") {
		t.Fatalf("restart did not pick up the row: %v", s.args)
	}
}

func TestChatInterruptKeepsProcessWhenClaudeConfirms(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	_ = a.ChatSend(c.ID, "hi", nil)
	fake.ctrlReply = "success"
	if err := a.ChatInterrupt(c.ID); err != nil {
		t.Fatal(err)
	}
	if !fake.Alive("1") {
		t.Fatal("a confirmed interrupt must keep the process")
	}
	old := claudeControlTimeout
	claudeControlTimeout = 20 * time.Millisecond
	t.Cleanup(func() { claudeControlTimeout = old })
	fake.ctrlReply = ""
	_ = a.ChatInterrupt(c.ID)
	if fake.Alive("1") {
		t.Fatal("an unanswered interrupt must fall back to a stop")
	}
}

func TestRPCSessionIDLandsOnRow(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "acp", AgentKind: "gem"})
	a.emitSession("1", "sess-acp", nil, nil)
	if row, _ := a.chatByID(c.ID); row.ClaudeSessionID != "sess-acp" {
		t.Fatalf("session id not recorded: %+v", row)
	}
}
