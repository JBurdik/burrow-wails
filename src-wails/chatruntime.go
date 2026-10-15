package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"burrow/internal/agentphase"
)

// The chat runtime: the Go-side owner of a chat's agent process, keyed by chat
// id (docs/plans/006-chat-runtime.md, decisions 1/5/6/11/12).
//
// A chat's process used to be started by a mounted AgentChat.vue, three times
// over (restartClaude / clearChat / ensureRuntime, each with its own drifted
// claude_start payload), plus copies in the phone store and RemoteCreateChat.
// So `spawn`, `chat_send` and the phone needed a desktop view to exist. Here
// every start reads the chat ROW (launch settings, claude_session_id) and
// resolves it the same way (claudeLaunchFor), whoever asked.
//
// All three transports: claude-cli (agentproc), Codex app-server and generic
// ACP (acp.go). Each starts from the row the same way whoever asked.

type chatRuntime struct {
	locks sync.Map // chat id string → *sync.Mutex

	ctrlSeq     atomic.Int64
	ctrlMu      sync.Mutex
	ctrlWaiters map[string]chan string // control request id → error text ("" = success)
}

// lock serialises ensure/send/restart/clear per chat, so two concurrent sends
// to a cold chat start ONE process (the singleflight the plan asks for) and a
// clear can't interleave with a send.
func (r *chatRuntime) lock(id string) func() {
	m, _ := r.locks.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// chatRuntimeKind is the stream kind for runtime lifecycle markers Burrow
// writes itself: "reset" (process stopped/restarted) and "clear" (transcript
// wiped). Its own kind for the same reason as chatUserKind — nothing a
// provider says can be mistaken for it. Persisted like any line, so a replay
// drops the same pending requests the live client did.
const chatRuntimeKind = "chat-runtime"

func (a *App) chatByID(id int64) (Chat, error) {
	if a.db == nil {
		return Chat{}, fmt.Errorf("no database")
	}
	c, err := scanChat(a.db.QueryRow(`SELECT `+chatColumns+` FROM chats WHERE id = ?`, id))
	if err != nil {
		return Chat{}, fmt.Errorf("chat %d: %w", id, err)
	}
	return c, nil
}

func (a *App) chatCwd(c Chat) string {
	var path string
	if a.db != nil {
		_ = a.db.QueryRow(`SELECT path FROM workspaces WHERE id = ?`, c.WorkspaceID).Scan(&path)
	}
	return path
}

// chatIsClaude: an empty transport is a chat from before the column, which
// was always the native Claude runtime unless its agent says otherwise.
func chatIsClaude(c Chat) bool {
	return c.Transport == "claude-cli" || (c.Transport == "" && (c.AgentKind == "" || c.AgentKind == "claude"))
}

// chatTransport is the runtime a row runs on: claude-cli, codex-app-server or
// acp (an unknown non-Claude transport is driven as generic ACP).
func chatTransport(c Chat) string {
	switch {
	case chatIsClaude(c):
		return "claude-cli"
	case c.Transport == "codex-app-server":
		return c.Transport
	}
	return "acp"
}

func (a *App) ensureClaude(c Chat) error {
	id := strconv.FormatInt(c.ID, 10)
	if a.claudeMgr().Alive(id) {
		return nil
	}
	cwd := a.chatCwd(c)
	return a.startClaude(id, cwd, c.ClaudeSessionID, a.claudeLaunchFor(c, cwd))
}

// startRPC starts a Codex app-server or ACP adapter session and registers it
// (acp.go). A field so tests can register a fake session instead of spawning.
func (a *App) startRPC(opts AcpStartOpts, transport string) error {
	if a.rpcStarter != nil {
		return a.rpcStarter(opts, transport)
	}
	if transport == "codex-app-server" {
		return a.CodexStart(opts.ID, opts.Cwd, opts.Env, opts.ResumeSessionID)
	}
	return a.AcpStart(opts)
}

// ensureRPC is ensureClaude for Codex / generic ACP: start from the row (its
// provider instance, resumed thread/session), then push the row's model,
// effort and permission mode into the fresh session — a start's defaults are
// the adapter's, not the chat's. The view used to do this after its own start
// (restoreAcpSelections, restoreSubagentCodexSettings); here it also happens
// for a spawned sub-agent or a phone chat nobody is looking at.
//
// The permission mode is the one that fails closed: Codex starts every thread
// in Auto, so a mode that does not apply would run the chat MORE permissively
// than its row says. Model and effort are best-effort.
func (a *App) ensureRPC(c Chat, transport string) error {
	id := strconv.FormatInt(c.ID, 10)
	if _, live := a.acpReg().get(id); live {
		return nil
	}
	opts, err := a.rpcLaunchFor(c, transport, a.chatCwd(c))
	if err != nil {
		return err
	}
	if err := a.startRPC(opts, transport); err != nil {
		return err
	}
	if c.Model != "" {
		_, _ = a.AcpSetConfig(id, "model", c.Model)
	}
	if c.Effort != "" {
		_, _ = a.AcpSetConfig(id, "effort", c.Effort)
	}
	if c.PermissionMode != "" {
		if _, err := a.AcpSetMode(id, c.PermissionMode); err != nil {
			_ = a.AcpStop(id)
			return fmt.Errorf("set permission mode %q: %w", c.PermissionMode, err)
		}
	}
	return nil
}

// ChatSend is the one door for a prompt, whatever the transport: it starts the
// process if there is none (resuming the row's session — also what makes a
// send after agentproc.ReapIdle work) and then sends.
func (a *App) ChatSend(chatID int64, text string, images []string) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if t := chatTransport(c); t != "claude-cli" {
		if err := a.ensureRPC(c, t); err != nil {
			return fmt.Errorf("start %s: %w", t, err)
		}
		_, err := a.AcpSend(id, text, images)
		return err
	}
	if err := a.ensureClaude(c); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	return a.ClaudeSend(id, text, c.ClaudeSessionID, images)
}

// stopProcess stops whichever runtime holds the chat id. By id, not by the
// row's transport: an agent switch rewrites the row while the old process may
// still be up.
func (a *App) stopProcess(id string) {
	if a.claudeMgr().Alive(id) {
		_ = a.ClaudeStop(id) // also settles the phase (Interrupt)
	}
	if _, live := a.acpReg().get(id); live {
		_ = a.AcpStop(id) // ditto
	}
}

// stopAndReset stops the process (if any) and publishes the reset, so every
// client drops its pending requests and spinner. Called with the chat lock.
func (a *App) stopAndReset(id, marker string) {
	a.stopProcess(id)
	a.emitChatLine(id, chatRuntimeKind, marker)
}

// ChatRestart stops the chat's process and emits session.reset. The next send
// starts a fresh one with --resume and whatever the row says now — lazily,
// because an idle CLI holds ~150 MB and a restart is usually an abort.
func (a *App) ChatRestart(chatID int64) error {
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	a.stopAndReset(id, "reset")
	return nil
}

// codexInterruptGrace is how long an accepted Codex turn/interrupt may take to
// end the turn before the runtime restarts the app-server instead.
var codexInterruptGrace = 5 * time.Second

// ChatInterrupt ends the running turn, keeping the process where the provider
// can stop a turn in place:
//   - Claude: an `interrupt` control request; unconfirmed → restart.
//   - Codex: `turn/interrupt`; the turn then ends through its own
//     turn/completed. Refused (no addressable turn yet) → restart; accepted but
//     still running after codexInterruptGrace → restart.
//   - generic ACP: restart.
//
// A restart always publishes the reset, which drops whatever request the dead
// turn was blocked on. So does a Claude interrupt.
func (a *App) ChatInterrupt(chatID int64) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if a.claudeMgr().Alive(id) {
		a.applyChatPhase(id, agentphase.Event{Kind: agentphase.Interrupt})
		if err := a.claudeControl(id, map[string]any{"subtype": "interrupt"}); err != nil {
			_ = a.ClaudeStop(id)
		}
		a.emitChatLine(id, chatRuntimeKind, "reset")
		return nil
	}
	if sess, live := a.acpReg().get(id); live && chatTransport(c) == "codex-app-server" {
		if a.CodexInterrupt(id) == nil {
			go a.restartIfStillRunning(chatID, sess)
			return nil
		}
	}
	a.stopAndReset(id, "reset")
	return nil
}

// restartIfStillRunning is the interrupt's fallback: a Codex that accepted
// turn/interrupt but never finished the turn gets restarted. Only the same
// session counts — a restart in between already settled it.
func (a *App) restartIfStillRunning(chatID int64, sess *acpSession) {
	time.Sleep(codexInterruptGrace)
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	sess.mu.Lock()
	running := sess.pendingTurn != 0
	sess.mu.Unlock()
	if cur, live := a.acpReg().get(id); live && cur == sess && running {
		a.stopAndReset(id, "reset")
	}
}

// ChatClear wipes the transcript (chat_messages + chat_stream), forgets the
// agent session so the next start is fresh, stops the process and tells every
// client. The client keeps only its own draft/images to drop.
func (a *App) ChatClear(chatID int64) error {
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	a.stopProcess(id)
	if err := a.DeleteChatMessages(int(chatID)); err != nil {
		return err
	}
	if _, err := a.db.Exec(`UPDATE chats SET claude_session_id = '', message_count = 0 WHERE id = ?`, chatID); err != nil {
		return err
	}
	a.emitChatLine(id, chatRuntimeKind, "clear")
	busEmit("chats-changed", nil)
	return nil
}

// LaunchSettingsPatch: nil = leave as is, "" = clear (fall back to defaults).
type LaunchSettingsPatch struct {
	Model              *string `json:"model,omitempty"`
	Effort             *string `json:"effort,omitempty"`
	PermissionMode     *string `json:"permission_mode,omitempty"`
	ProfileID          *string `json:"profile_id,omitempty"`
	AppendSystemPrompt *string `json:"append_system_prompt,omitempty"`
	// RowOnly records the settings without touching a live process: the
	// process already runs that way (Claude left plan mode itself after a plan
	// approval), or the client is only seeding the row for the next start.
	RowOnly bool `json:"row_only,omitempty"`
}

// ChatSetLaunchSettings writes the row, then applies the change to a live
// process where the provider can take it in place — Claude set_model /
// set_permission_mode (confirmed by the CLI, not assumed), Codex / ACP through
// applyRPCSettings; anything else, or a refusal, restarts so the next send
// starts with the new settings. A cold chat just has its row updated: every
// start reads it.
func (a *App) ChatSetLaunchSettings(chatID int64, patch LaunchSettingsPatch) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()

	sets, args := []string{}, []any{}
	set := func(col string, v *string, cur string) bool {
		if v == nil || *v == cur {
			return false
		}
		sets, args = append(sets, col+" = ?"), append(args, *v)
		return true
	}
	model := set("model", patch.Model, c.Model)
	perm := set("permission_mode", patch.PermissionMode, c.PermissionMode)
	effort := set("effort", patch.Effort, c.Effort)
	other := set("profile_id", patch.ProfileID, c.ProfileID)
	other = set("append_system_prompt", patch.AppendSystemPrompt, c.AppendSystemPrompt) || other
	if len(sets) == 0 {
		return nil
	}
	// Column names come from the literal calls above, never from input.
	if _, err := a.db.Exec(`UPDATE chats SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, chatID)...); err != nil {
		return err
	}
	busEmit("chats-changed", nil)

	if patch.RowOnly {
		return nil
	}
	if !chatIsClaude(c) {
		a.applyRPCSettings(id, patch, other)
		return nil
	}
	if !a.claudeMgr().Alive(id) {
		return nil
	}
	live := !other && !effort // Claude has no live effort control
	if live && model {
		m := *patch.Model
		if m == "" || !strings.HasPrefix(m, "claude-") {
			live = false // "back to the CLI default" has no control request
		} else if a.claudeControl(id, map[string]any{"subtype": "set_model", "model": m}) != nil {
			live = false
		}
	}
	if live && perm {
		if a.claudeControl(id, map[string]any{"subtype": "set_permission_mode", "mode": claudePermMode(*patch.PermissionMode)}) != nil {
			live = false
		}
	}
	if !live {
		a.stopAndReset(id, "reset")
	}
	return nil
}

// applyRPCSettings is ChatSetLaunchSettings' live half for Codex / ACP: model
// and effort through AcpSetConfig (Codex stashes them for the next turn/start,
// an ACP adapter switches in place), the mode through AcpSetMode. A profile or
// system-prompt change, or a mode the session refuses, restarts — the next
// start applies the row (ensureRPC). A cold chat has nothing to apply.
func (a *App) applyRPCSettings(id string, patch LaunchSettingsPatch, restart bool) {
	if _, live := a.acpReg().get(id); !live {
		return
	}
	if !restart && patch.Model != nil && *patch.Model != "" {
		_, _ = a.AcpSetConfig(id, "model", *patch.Model)
	}
	if !restart && patch.Effort != nil && *patch.Effort != "" {
		_, _ = a.AcpSetConfig(id, "effort", *patch.Effort)
	}
	if !restart && patch.PermissionMode != nil && *patch.PermissionMode != "" {
		_, err := a.AcpSetMode(id, *patch.PermissionMode)
		restart = err != nil
	}
	if restart {
		a.stopAndReset(id, "reset")
	}
}

// recordChatSessionID puts the agent's session / thread id on the row, where
// every start reads its resume target.
func (a *App) recordChatSessionID(id, sessionID string) {
	chatID := chatIDOf(id)
	if chatID <= 0 || sessionID == "" || a.db == nil {
		return
	}
	res, err := a.db.Exec(`UPDATE chats SET claude_session_id = ? WHERE id = ? AND claude_session_id != ?`, sessionID, chatID, sessionID)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			busEmit("chats-changed", nil)
		}
	}
}

// claudeControlTimeout bounds a control request. A CLI that never answers is
// treated as a refusal — every caller has a restart to fall back on.
var claudeControlTimeout = 5 * time.Second

// claudeControl sends a control_request on the CLI's stdin and waits for its
// control_response (observeClaudeLine routes it here by request_id).
func (a *App) claudeControl(id string, request map[string]any) error {
	r := &a.chatRT
	rid := fmt.Sprintf("burrow-%d", r.ctrlSeq.Add(1))
	ch := make(chan string, 1)
	r.ctrlMu.Lock()
	if r.ctrlWaiters == nil {
		r.ctrlWaiters = map[string]chan string{}
	}
	r.ctrlWaiters[rid] = ch
	r.ctrlMu.Unlock()
	defer func() {
		r.ctrlMu.Lock()
		delete(r.ctrlWaiters, rid)
		r.ctrlMu.Unlock()
	}()
	payload, err := json.Marshal(map[string]any{"type": "control_request", "request_id": rid, "request": request})
	if err != nil {
		return err
	}
	if err := a.claudeWrite(id, string(payload)); err != nil {
		return err
	}
	select {
	case msg := <-ch:
		if msg != "" {
			return errors.New(msg)
		}
		return nil
	case <-time.After(claudeControlTimeout):
		return fmt.Errorf("claude did not answer %v", request["subtype"])
	}
}

// observeClaudeLine is the runtime's own reading of the CLI's stdout, beside
// emitChatLine: it captures the session id onto the row (the next start
// resumes it with no client involved) and completes control requests.
// A cheap substring test first — this runs on every streamed token.
func (a *App) observeClaudeLine(id, line string) {
	isInit := strings.Contains(line, `"init"`)
	isCtrl := strings.Contains(line, `"control_response"`)
	if !isInit && !isCtrl {
		return
	}
	var ev struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		Response  struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(line), &ev) != nil {
		return
	}
	switch {
	case ev.Type == "system" && ev.Subtype == "init" && ev.SessionID != "":
		a.recordChatSessionID(id, ev.SessionID)
	case ev.Type == "control_response":
		r := &a.chatRT
		r.ctrlMu.Lock()
		ch := r.ctrlWaiters[ev.Response.RequestID]
		r.ctrlMu.Unlock()
		if ch == nil {
			return
		}
		msg := ""
		if ev.Response.Subtype != "success" {
			msg = ev.Response.Error
			if msg == "" {
				msg = "claude refused the control request"
			}
		}
		select {
		case ch <- msg:
		default:
		}
	}
}
