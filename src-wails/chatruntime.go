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
// claude-cli only for now. Codex and generic ACP still start from the view
// until PR4; ChatSend refuses them rather than guessing a start payload.

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

func (a *App) ensureClaude(c Chat) error {
	id := strconv.FormatInt(c.ID, 10)
	if a.claudeMgr().Alive(id) {
		return nil
	}
	cwd := a.chatCwd(c)
	return a.startClaude(id, cwd, c.ClaudeSessionID, a.claudeLaunchFor(c, cwd))
}

// ChatSend is the one door for a prompt: it starts the process if there is
// none (resuming the row's claude_session_id — also what makes a send after
// agentproc.ReapIdle work) and then sends.
func (a *App) ChatSend(chatID int64, text string, images []string) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	if !chatIsClaude(c) {
		return fmt.Errorf("chat %d runs on %q, which the chat runtime does not start yet", chatID, c.Transport)
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if err := a.ensureClaude(c); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	return a.ClaudeSend(id, text, c.ClaudeSessionID, images)
}

// stopAndReset stops the process (if any) and publishes the reset, so every
// client drops its pending requests and spinner. Called with the chat lock.
func (a *App) stopAndReset(id, marker string) {
	if a.claudeMgr().Alive(id) {
		_ = a.ClaudeStop(id) // also settles the phase (Interrupt)
	}
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

// ChatInterrupt ends the running turn but keeps the process: a Claude
// `interrupt` control request. If the CLI does not confirm, it falls back to a
// restart so the stop button is never a no-op. Either way the reset follows,
// which drops whatever request the dead turn was blocked on.
func (a *App) ChatInterrupt(chatID int64) error {
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if a.claudeMgr().Alive(id) {
		a.applyChatPhase(id, agentphase.Event{Kind: agentphase.Interrupt})
		if err := a.claudeControl(id, map[string]any{"subtype": "interrupt"}); err != nil {
			_ = a.ClaudeStop(id)
		}
	}
	a.emitChatLine(id, chatRuntimeKind, "reset")
	return nil
}

// ChatClear wipes the transcript (chat_messages + chat_stream), forgets the
// Claude session so the next start is fresh, stops the process and tells every
// client. The client keeps only its own draft/images to drop.
func (a *App) ChatClear(chatID int64) error {
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if a.claudeMgr().Alive(id) {
		_ = a.ClaudeStop(id)
	}
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
// process where Claude can take it in place (set_model, set_permission_mode —
// confirmed by the CLI, not assumed); anything else, or a refusal, restarts so
// the next send starts with the new settings. A cold chat just has its row
// updated: every start reads it.
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
	other := set("effort", patch.Effort, c.Effort)
	other = set("profile_id", patch.ProfileID, c.ProfileID) || other
	other = set("append_system_prompt", patch.AppendSystemPrompt, c.AppendSystemPrompt) || other
	if len(sets) == 0 {
		return nil
	}
	// Column names come from the literal calls above, never from input.
	if _, err := a.db.Exec(`UPDATE chats SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, chatID)...); err != nil {
		return err
	}
	busEmit("chats-changed", nil)

	if patch.RowOnly || !chatIsClaude(c) || !a.claudeMgr().Alive(id) {
		return nil
	}
	live := !other
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
		chatID := chatIDOf(id)
		if chatID <= 0 || a.db == nil {
			return
		}
		res, err := a.db.Exec(`UPDATE chats SET claude_session_id = ? WHERE id = ? AND claude_session_id != ?`, ev.SessionID, chatID, ev.SessionID)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				busEmit("chats-changed", nil)
			}
		}
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
