package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Side chats: a quick question about a chat that must not disturb it.
//
// A side chat is a row with parent_chat_id = the chat it asks about and
// kind = "side". It starts from the parent's CURRENT agent session — full
// context, no transcript copied into the side view — by forking it, so the
// parent's session, process and running turn are never touched:
//   - Claude: `--resume <parent session> --fork-session` (no
//     --resume-session-at: the fork is at the session's end). The CLI reads
//     the parent's append-only session file, so this works while the parent is
//     mid-turn — an in-flight tool call shows up in the fork as stopped.
//   - Codex: `thread/fork` of the parent's thread id from a separate
//     app-server, which loads the thread from its rollout on disk.
//   - generic ACP: no fork in the protocol — refused.
//
// The fork happens on the side chat's FIRST start (resume_at = sideForkLatest,
// read by ensureClaude / rpcLaunchFor), and recordChatSessionID clears it when
// the forked session reports its own id — after that the side chat resumes its
// own session like any chat.
//
// Not a sub-agent: collect_results skips kind = 'side' (UncollectedChildren)
// and the Right Panel's Sub-agents list filters it out (chatTree.ts).

// sideForkLatest is the resume_at value that means "fork the resumed session
// at its end" — never a Claude record uuid.
const sideForkLatest = "@latest"

const chatKindSide = "side"

func sideChatSupported(c Chat) error {
	if c.Kind == chatKindSide {
		return fmt.Errorf("a side chat can't have side chats of its own")
	}
	if chatTransport(c) == "acp" {
		return fmt.Errorf("side chats need a Claude or Codex chat")
	}
	if c.ClaudeSessionID == "" {
		return fmt.Errorf("nothing to fork yet: the chat has no agent session (send it a message first)")
	}
	return nil
}

// ChatSideCreate returns a side chat of chatID, ready for its first send.
// A side chat of this parent that was never started (no transcript, fork still
// pending) is reused rather than piling up empty rows on every click — its
// fork point is moved to the parent's current session.
func (a *App) ChatSideCreate(chatID int64) (Chat, error) {
	if a.db == nil {
		return Chat{}, fmt.Errorf("no database")
	}
	parent, err := a.chatByID(chatID)
	if err != nil {
		return Chat{}, err
	}
	if err := sideChatSupported(parent); err != nil {
		return Chat{}, err
	}

	var unused int64
	err = a.db.QueryRow(`SELECT id FROM chats
		WHERE parent_chat_id = ? AND kind = ? AND archived_at = 0 AND resume_at = ?
		  AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.chat_id = chats.id)
		  AND NOT EXISTS (SELECT 1 FROM chat_stream s WHERE s.chat_id = CAST(chats.id AS TEXT))
		ORDER BY id DESC LIMIT 1`, parent.ID, chatKindSide, sideForkLatest).Scan(&unused)
	switch {
	case err == nil:
		if _, err := a.db.Exec(`UPDATE chats SET claude_session_id = ?, last_activity_at = ? WHERE id = ?`,
			parent.ClaudeSessionID, time.Now().UnixMilli(), unused); err != nil {
			return Chat{}, err
		}
		busEmit("chats-changed", nil)
		return a.chatByID(unused)
	case !errors.Is(err, sql.ErrNoRows):
		return Chat{}, err
	}

	child, err := a.CreateChat(Chat{
		WorkspaceID:        parent.WorkspaceID,
		Title:              "Side chat",
		ClaudeSessionID:    parent.ClaudeSessionID,
		AgentKind:          parent.AgentKind,
		Transport:          parent.Transport,
		Model:              parent.Model,
		Branch:             parent.Branch,
		LastActivityAt:     time.Now().UnixMilli(),
		ParentChatID:       parent.ID,
		Effort:             parent.Effort,
		PermissionMode:     parent.PermissionMode,
		ProfileID:          parent.ProfileID,
		AppendSystemPrompt: parent.AppendSystemPrompt,
	})
	if err != nil {
		return Chat{}, err
	}
	if _, err := a.db.Exec(`UPDATE chats SET kind = ?, resume_at = ? WHERE id = ?`, chatKindSide, sideForkLatest, child.ID); err != nil {
		return Chat{}, err
	}
	busEmit("chats-changed", nil)
	return a.chatByID(child.ID)
}
