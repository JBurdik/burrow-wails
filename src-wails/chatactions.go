package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"burrow/internal/control"
)

// Reply buttons (threadbear's): an agent puts a ```action block in its reply —
// {label, kind: "new_thread"|"send", brief, chat_id?} — and the client draws a
// button where the block is. Pressing goes through Go (ChatPressAction): the
// brief and the target are read from the STORED reply, never from the client;
// a press runs once (CAS on chat_action_presses); the writing thread's
// capability gate is applied at press time, with that thread as the caller.
// "new_thread" = a sub-agent of the writing thread (the spawn verb's shape),
// "send" = chat_submit into chat_id. The next message the user submits to the
// writing thread carries "[Pressed: <label>]" lines, so a "go ahead" doesn't
// start the same job twice.

type replyAction struct {
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Brief  string `json:"brief"`
	ChatID int64  `json:"chat_id,omitempty"`
}

// ChatActionPress is one pressed button: state running | done | failed.
type ChatActionPress struct {
	ChatID       int64  `json:"chat_id"`
	MsgIndex     int    `json:"msg_index"`
	ActionIndex  int    `json:"action_index"`
	Label        string `json:"label"`
	State        string `json:"state"`
	ResultChatID int64  `json:"result_chat_id"`
	Error        string `json:"error,omitempty"`
	PressedAt    int64  `json:"pressed_at"`
}

func chatActionsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_action_presses (
			chat_id        INTEGER NOT NULL,
			msg_index      INTEGER NOT NULL,
			action_index   INTEGER NOT NULL,
			label          TEXT    NOT NULL DEFAULT '',
			state          TEXT    NOT NULL,
			result_chat_id INTEGER NOT NULL DEFAULT 0,
			error          TEXT    NOT NULL DEFAULT '',
			pressed_at     INTEGER NOT NULL,
			reported       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (chat_id, msg_index, action_index)
		)`,
	}
}

var actionFenceOpen = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*([^\\s`]*)")

// parseReplyActions returns a reply's ```action blocks in document order —
// the order the client's markdown renderer numbers them in. Only closed
// fences count (a streaming block is a code block); a fence nested in another
// fence is content. A block that does not parse still takes its index (the
// client renders it as a plain code block and numbers it too) as nil.
// ponytail: line-based, not CommonMark — an ```action inside a blockquote is
// numbered by the client but not here; add when agents write that.
func parseReplyActions(text string) []*replyAction {
	var out []*replyAction
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := actionFenceOpen.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		fence, lang := m[1], m[2]
		closeRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(fence[:1]) + `{` + strconv.Itoa(len(fence)) + `,}\s*$`)
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if closeRe.MatchString(lines[j]) {
				end = j
				break
			}
		}
		if end < 0 {
			break // unclosed: the rest is code
		}
		if lang == "action" {
			out = append(out, parseReplyAction(strings.Join(lines[i+1:end], "\n")))
		}
		i = end
	}
	return out
}

func parseReplyAction(body string) *replyAction {
	var a replyAction
	if json.Unmarshal([]byte(body), &a) != nil {
		return nil
	}
	a.Label, a.Brief = strings.TrimSpace(a.Label), strings.TrimSpace(a.Brief)
	if a.Label == "" || a.Brief == "" {
		return nil
	}
	switch a.Kind {
	case "new_thread":
	case "send":
		if a.ChatID <= 0 {
			return nil
		}
	default:
		return nil
	}
	return &a
}

// pressedNote is what the user's next message to the writing thread carries.
func pressedNote(labels []string) string {
	var b strings.Builder
	for _, l := range labels {
		b.WriteString("\n[Pressed: " + l + "]")
	}
	return b.String()
}

// ChatPressAction presses button actionIndex of reply msgIndex in chatID.
func (a *App) ChatPressAction(chatID int64, msgIndex, actionIndex int) (ChatActionPress, error) {
	var none ChatActionPress
	if a.db == nil {
		return none, fmt.Errorf("no database")
	}
	writer, err := a.chatByID(chatID)
	if err != nil {
		return none, err
	}
	msgs, err := a.chatTranscript(chatID)
	if err != nil {
		return none, err
	}
	if msgIndex < 0 || msgIndex >= len(msgs) || msgs[msgIndex].Role != "assistant" || msgs[msgIndex].Partial {
		return none, fmt.Errorf("reply %d not found", msgIndex)
	}
	acts := parseReplyActions(msgs[msgIndex].Text)
	if actionIndex < 0 || actionIndex >= len(acts) || acts[actionIndex] == nil {
		return none, fmt.Errorf("button %d not found in reply %d", actionIndex, msgIndex)
	}
	act := acts[actionIndex]

	// Claim: a new row, or a failed one again (retry). Anything else = pressed.
	now := time.Now().UnixMilli()
	res, err := a.db.Exec(`INSERT INTO chat_action_presses (chat_id, msg_index, action_index, label, state, pressed_at)
		VALUES (?, ?, ?, ?, 'running', ?)
		ON CONFLICT(chat_id, msg_index, action_index) DO UPDATE SET state = 'running', error = '', pressed_at = excluded.pressed_at
		WHERE chat_action_presses.state = 'failed'`, chatID, msgIndex, actionIndex, act.Label, now)
	if err != nil {
		return none, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return a.chatActionPress(chatID, msgIndex, actionIndex)
	}
	a.emitActionPresses(chatID)

	result, runErr := a.runReplyAction(writer, act)
	state, msg := "done", ""
	if runErr != nil {
		state, msg = "failed", runErr.Error()
	}
	_, _ = a.db.Exec(`UPDATE chat_action_presses SET state = ?, result_chat_id = ?, error = ? WHERE chat_id = ? AND msg_index = ? AND action_index = ?`,
		state, result, msg, chatID, msgIndex, actionIndex)
	a.emitActionPresses(chatID)
	p, err := a.chatActionPress(chatID, msgIndex, actionIndex)
	if err == nil && runErr != nil {
		return p, runErr
	}
	return p, err
}

// runReplyAction performs a pressed button as the thread that wrote it.
func (a *App) runReplyAction(writer Chat, act *replyAction) (int64, error) {
	caller := capChat{ID: writer.ID, Parent: writer.ParentChatID, Mode: writer.PermissionMode}
	switch act.Kind {
	case "new_thread":
		if writer.ParentChatID > 0 {
			return 0, fmt.Errorf("a sub-agent's button cannot start a thread (sub-agents are one level deep)")
		}
		if p := capabilityProblem(capCall{Verb: "spawn", Caller: caller}); p != "" {
			return 0, fmt.Errorf("%s", p)
		}
		got, err := a.spawnSubagent(control.SpawnRequest{Task: act.Brief, ParentChatID: writer.ID})
		return got.ChatID, err
	default: // send
		t, err := a.chatByID(act.ChatID)
		if err != nil {
			return 0, err
		}
		if t.ArchivedAt != 0 {
			return 0, fmt.Errorf("chat %d is archived", t.ID)
		}
		c := capCall{Verb: "chat_send", Caller: caller, Target: &capChat{ID: t.ID, Parent: t.ParentChatID, Mode: t.PermissionMode}}
		if p := capabilityProblem(c); p != "" {
			return 0, fmt.Errorf("%s", p)
		}
		_, err = a.ChatSubmit(t.ID, act.Brief, nil, "")
		return t.ID, err
	}
}

func (a *App) chatActionPress(chatID int64, msgIndex, actionIndex int) (ChatActionPress, error) {
	var p ChatActionPress
	err := a.db.QueryRow(`SELECT chat_id, msg_index, action_index, label, state, result_chat_id, error, pressed_at
		FROM chat_action_presses WHERE chat_id = ? AND msg_index = ? AND action_index = ?`, chatID, msgIndex, actionIndex).
		Scan(&p.ChatID, &p.MsgIndex, &p.ActionIndex, &p.Label, &p.State, &p.ResultChatID, &p.Error, &p.PressedAt)
	return p, err
}

// ChatActionPresses: every pressed button of a chat (clients key them by
// msg_index / action_index).
func (a *App) ChatActionPresses(chatID int64) ([]ChatActionPress, error) {
	out := []ChatActionPress{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(`SELECT chat_id, msg_index, action_index, label, state, result_chat_id, error, pressed_at
		FROM chat_action_presses WHERE chat_id = ? ORDER BY msg_index, action_index`, chatID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ChatActionPress
		if rows.Scan(&p.ChatID, &p.MsgIndex, &p.ActionIndex, &p.Label, &p.State, &p.ResultChatID, &p.Error, &p.PressedAt) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (a *App) emitActionPresses(chatID int64) {
	list, _ := a.ChatActionPresses(chatID)
	busEmit("chat-action-presses", map[string]any{"chat_id": chatID, "presses": list})
}

// takePressedNote: the "[Pressed: …]" lines for presses not yet reported to
// chatID's agent, and a func that marks them reported (call on success).
func (a *App) takePressedNote(chatID int64) (string, func()) {
	nop := func() {}
	if a.db == nil {
		return "", nop
	}
	rows, err := a.db.Query(`SELECT msg_index, action_index, label FROM chat_action_presses
		WHERE chat_id = ? AND reported = 0 AND state = 'done' ORDER BY pressed_at`, chatID)
	if err != nil {
		return "", nop
	}
	defer rows.Close()
	var labels []string
	var keys [][2]int
	for rows.Next() {
		var m, i int
		var l string
		if rows.Scan(&m, &i, &l) == nil {
			labels, keys = append(labels, l), append(keys, [2]int{m, i})
		}
	}
	if len(labels) == 0 {
		return "", nop
	}
	return pressedNote(labels), func() {
		for _, k := range keys {
			_, _ = a.db.Exec(`UPDATE chat_action_presses SET reported = 1 WHERE chat_id = ? AND msg_index = ? AND action_index = ?`, chatID, k[0], k[1])
		}
	}
}

// dropActionPresses forgets presses at or after a transcript cut (rewind,
// clear, delete): those indexes will hold other rows.
func (a *App) dropActionPresses(chatID int64, fromIndex int) {
	if a.db == nil {
		return
	}
	if res, err := a.db.Exec(`DELETE FROM chat_action_presses WHERE chat_id = ? AND msg_index >= ?`, chatID, fromIndex); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			a.emitActionPresses(chatID)
		}
	}
}
