package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"burrow/internal/agentphase"
)

// Steer: a prompt delivered INTO the running turn instead of queued behind it.
//
//   - Claude CLI (stream-json input): a `type:"user"` line written while a turn
//     runs is picked up at the next tool boundary and the turn continues — one
//     `result`, one turn end (verified against claude 2.1.311: a steer written
//     after the first tool_use was obeyed before the remaining planned calls).
//     So a steer is the ordinary ClaudeSend, gated on the turn being in flight.
//   - Codex app-server: `turn/steer {threadId, input, expectedTurnId}` →
//     `{turnId}`; the request fails when expectedTurnId is not the active turn.
//   - generic ACP: no such thing — refused, and the UI hides the action.
//
// The prompt is recorded as chatUserKind like any send, so the transcript shows
// it where it happened, and turnNotesFor joins it to the open turn (a user line
// while a turn is open never opens a second one).
//
// Only a RUNNING turn is steerable. A turn blocked on a permission / question
// is refused: the published user line maps to HookRunning, which would paint a
// pending approval as running and hide the very prompt the turn waits on.

// errNoTurnToSteer means the message should stay queued: the turn ended (or
// never started) and the queue drain will send it as its own turn.
var errNoTurnToSteer = errors.New("no running turn to steer")

// ChatSteer delivers text into chatID's running turn. On any error nothing was
// sent and nothing was recorded.
func (a *App) ChatSteer(chatID int64, text string, images []string) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if !a.chatTurnRunning(id) {
		return errNoTurnToSteer
	}
	switch t := chatTransport(c); t {
	case "claude-cli":
		if !a.claudeMgr().Alive(id) {
			return errNoTurnToSteer
		}
		if err := a.ClaudeSend(id, text, c.ClaudeSessionID, images); err != nil {
			return err
		}
	case "codex-app-server":
		if err := a.codexSteer(id, text, images); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s cannot take a message mid-turn", t)
	}
	a.chatRT.touchChat(id, time.Now())
	return nil
}

// chatTurnRunning: the chat's phase says a turn is running and not blocked on
// the user. No phase store (some tests) → trust the process checks alone.
func (a *App) chatTurnRunning(id string) bool {
	if a.phases == nil {
		return true
	}
	return a.phases.Get("chat:"+id).State == agentphase.Running
}

// codexSteer sends `turn/steer` for the session's running turn and records the
// prompt once Codex accepted it.
func (a *App) codexSteer(id, text string, images []string) error {
	sess, ok := a.acpReg().get(id)
	if !ok || sess.proto != protoCodexAppServer || sess.native != nil {
		return errNoTurnToSteer
	}
	sess.mu.Lock()
	turnID, pending := sess.turnID, sess.pendingTurn
	sess.mu.Unlock()
	// turnID arrives with the turn/start ack / turn/started; before that there
	// is nothing to address, after turn/completed pendingTurn is back to 0.
	if turnID == "" || pending == 0 {
		return errNoTurnToSteer
	}
	input := []any{}
	if text != "" {
		input = append(input, map[string]any{"type": "text", "text": text})
	}
	for _, uri := range images {
		input = append(input, map[string]any{"type": "image", "url": uri})
	}
	if _, err := codexCall(sess, "turn/steer", map[string]any{
		"threadId": sess.sessionID, "input": input, "expectedTurnId": turnID,
	}); err != nil {
		return fmt.Errorf("codex refused the steer: %w", err)
	}
	a.resetCodexTurnWatchdog(id, sess)
	a.emitChatLine(id, chatUserKind, text)
	return nil
}
