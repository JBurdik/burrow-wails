package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Rewind and fork of a Claude chat at a transcript message.
//
// Both ride one hidden Claude CLI flag pair, verified against 2.1.300:
// `--resume <session> --resume-session-at <record uuid> --fork-session` loads
// the session only up to that record and continues under a NEW session id,
// leaving the resumed session's file untouched. The uuid comes from the
// folded transcript (ChatMessage.UUID, the latest stream-json record that
// touched a message). So:
//   - rewind: truncate this chat's transcript before a user message and park
//     the uuid in chats.resume_at; the next start forks from there.
//   - fork: a new chat row whose claude_session_id is the parent's and whose
//     resume_at is the cut — the parent's session and transcript stay as they
//     are.
//
// recordChatSessionID clears resume_at when the forked session reports its
// own id. Codex / generic ACP have no equivalent, so both refuse them.

// chatCut validates that msgs[index] is the message the client means and
// returns how much of the transcript a rewind or fork keeps:
//   - rewind: everything before the user message at index.
//   - fork: through the end of the turn holding index (up to the next user
//     message), so a fork from a prompt includes the agent's answer to it.
func chatCut(msgs []ChatMessage, index int, text string, fork bool) (int, error) {
	if index < 0 || index >= len(msgs) {
		return 0, fmt.Errorf("message %d is not in the transcript (reload the chat)", index)
	}
	m := msgs[index]
	if m.Text != text {
		return 0, fmt.Errorf("the transcript changed under message %d (reload the chat)", index)
	}
	if !fork {
		if m.Role != "user" {
			return 0, fmt.Errorf("can only rewind to one of your messages")
		}
		return index, nil
	}
	if m.Role != "user" && m.Role != "assistant" {
		return 0, fmt.Errorf("can only fork from a prompt or a reply")
	}
	end := index + 1
	for end < len(msgs) && msgs[end].Role != "user" && msgs[end].Role != "queued" {
		end++
	}
	return end, nil
}

// chatResumePoint is the record uuid that ends a kept transcript, or "" when
// it holds no agent output at all (the session starts fresh). Only the last
// turn is searched: a uuid from an earlier turn would silently drop the last
// one from the agent's memory while the transcript still shows it.
//
// ponytail: "latest uuid in the turn" assumes records arrive in transcript
// order. Parallel tool results finishing out of order could pick a uuid one
// record short of the turn's end; the turn's closing text always comes last,
// so in practice that only bites a turn that ended on a tool result.
func chatResumePoint(keep []ChatMessage) (string, error) {
	agentOutput := false
	for i := len(keep) - 1; i >= 0; i-- {
		switch keep[i].Role {
		case "user":
			if agentOutput {
				return "", fmt.Errorf("this point can't be restored: its turn has no agent record id (an older transcript or a failed turn)")
			}
			// A turn the agent never answered: keep looking at the one before.
		case "assistant", "thinking", "tool":
			if keep[i].UUID != "" {
				return keep[i].UUID, nil
			}
			agentOutput = true
		}
	}
	if agentOutput {
		return "", fmt.Errorf("this point can't be restored: the transcript has no agent record ids")
	}
	return "", nil
}

func messageCount(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role != "tool" {
			n++
		}
	}
	return n
}

func (a *App) chatTranscript(chatID int64) ([]ChatMessage, error) {
	raw, err := a.LoadChatMessages(int(chatID))
	if err != nil {
		return nil, err
	}
	var msgs []ChatMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		return nil, fmt.Errorf("decode transcript: %w", err)
	}
	return msgs, nil
}

func (a *App) chatResumeAt(chatID int64) string {
	var at string
	if a.db != nil {
		_ = a.db.QueryRow(`SELECT resume_at FROM chats WHERE id = ?`, chatID).Scan(&at)
	}
	return at
}

func claudeOnly(c Chat) error {
	if chatTransport(c) != "claude-cli" {
		return fmt.Errorf("rewind and fork need a Claude chat")
	}
	return nil
}

// ChatRewind drops the user message at index and everything after it, and
// makes the next send continue the agent's conversation from just before it.
// `text` must match the message (a guard against a client whose transcript
// has moved on). The client puts the text back in its composer.
func (a *App) ChatRewind(chatID int64, index int, text string) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	if err := claudeOnly(c); err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	a.stopProcess(id)

	msgs, err := a.chatTranscript(chatID)
	if err != nil {
		return err
	}
	cut, err := chatCut(msgs, index, text, false)
	if err != nil {
		return err
	}
	keep := msgs[:cut]
	resumeAt, err := chatResumePoint(keep)
	if err != nil {
		return err
	}
	if err := a.truncateChatTranscript(id, keep); err != nil {
		return err
	}
	// No agent output kept → no session to resume: start fresh.
	if _, err := a.db.Exec(
		`UPDATE chats SET resume_at = ?, message_count = ?,
			claude_session_id = CASE WHEN ? = '' THEN '' ELSE claude_session_id END
		 WHERE id = ?`, resumeAt, messageCount(keep), resumeAt, chatID); err != nil {
		return err
	}
	a.emitChatLine(id, chatRuntimeKind, "reset")
	busEmit("chats-changed", nil)
	return nil
}

// truncateChatTranscript replaces the transcript with keep and drops every
// stream line recorded so far. folded_ord moves to the next ord in the same
// transaction, so nothing already in chat_stream (or still queued for it) is
// ever folded or replayed back on top of the cut. Ords are NOT restarted, unlike
// a clear: the marker only ever moves forward.
func (a *App) truncateChatTranscript(id string, keep []ChatMessage) error {
	w := a.chatStream()
	if w == nil {
		return fmt.Errorf("no database")
	}
	next := w.peekNextOrd(id)
	payload, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	chatID, _ := strconv.Atoi(id)
	if err := a.SaveChatMessages(chatID, string(payload), next); err != nil {
		return err
	}
	w.flush()
	_, err = a.db.Exec(`DELETE FROM chat_stream WHERE chat_id = ? AND ord < ?`, id, next)
	return err
}

// peekNextOrd is the ord the next appended line will get, without taking it.
func (w *chatStreamWriter) peekNextOrd(chatID string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	ord, ok := w.nextOrd[chatID]
	if !ok {
		ord = w.loadMaxOrdLocked(chatID)
		w.nextOrd[chatID] = ord
	}
	return ord
}

// ChatFork creates a new chat that continues the conversation through the
// turn holding the message at index (see chatCut). Same workspace, agent and
// launch settings; the transcript up to the cut is copied; the first send
// forks the parent's Claude session at that point, which leaves the parent
// untouched. Returns the new chat.
func (a *App) ChatFork(chatID int64, index int, text string) (Chat, error) {
	parent, err := a.chatByID(chatID)
	if err != nil {
		return Chat{}, err
	}
	if err := claudeOnly(parent); err != nil {
		return Chat{}, err
	}
	msgs, err := a.chatTranscript(chatID)
	if err != nil {
		return Chat{}, err
	}
	cut, err := chatCut(msgs, index, text, true)
	if err != nil {
		return Chat{}, err
	}
	keep := msgs[:cut]
	resumeAt, err := chatResumePoint(keep)
	if err != nil {
		return Chat{}, err
	}
	if resumeAt == "" || parent.ClaudeSessionID == "" {
		return Chat{}, fmt.Errorf("nothing to fork yet: the agent has not answered up to this point")
	}
	child, err := a.CreateChat(Chat{
		WorkspaceID:        parent.WorkspaceID,
		Title:              parent.Title + " (fork)",
		PinnedTitle:        true,
		ClaudeSessionID:    parent.ClaudeSessionID,
		MessageCount:       int64(messageCount(keep)),
		AgentKind:          parent.AgentKind,
		Transport:          parent.Transport,
		Model:              parent.Model,
		Branch:             parent.Branch,
		LastActivityAt:     time.Now().UnixMilli(),
		Effort:             parent.Effort,
		PermissionMode:     parent.PermissionMode,
		ProfileID:          parent.ProfileID,
		AppendSystemPrompt: parent.AppendSystemPrompt,
	})
	if err != nil {
		return Chat{}, err
	}
	if _, err := a.db.Exec(`UPDATE chats SET resume_at = ? WHERE id = ?`, resumeAt, child.ID); err != nil {
		return Chat{}, err
	}
	payload, err := json.Marshal(keep)
	if err != nil {
		return Chat{}, err
	}
	if err := a.SaveChatMessages(int(child.ID), string(payload), -1); err != nil {
		return Chat{}, err
	}
	return child, nil
}
