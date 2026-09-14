package main

import "testing"

// MINOR: collected_at is bookkeeping for collect_results only (chats.go's
// UncollectedChildren filters on it), so a child collected once and then
// steered with chat_send into a NEW turn must not stay invisible to
// collect_results forever. clearCollectedOnRunning (chatstream.go) resets it
// the moment the chat's phase re-enters Running, mirroring
// clearSettledOverrideOnRunning right next to it.
func TestCollectedAtClearsWhenANewTurnStarts(t *testing.T) {
	a := newTestApp(t)
	const chatID = "63"

	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatalf("phase store: %v", err)
	}
	a.phases = store

	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, collected_at) VALUES (63, 1, 'sub-agent', 999)`,
	); err != nil {
		t.Fatal(err)
	}

	// A text delta is what HookRunning maps text.delta/tool.started/etc onto
	// (providerruntime.go) — the same signal a chat_send-driven new turn
	// produces once the CLI actually starts answering.
	a.emitChatLine(chatID, "claude-data", claudeTextLine("m1", "back to work"))

	var collectedAt int64
	if err := a.db.QueryRow(`SELECT collected_at FROM chats WHERE id = 63`).Scan(&collectedAt); err != nil {
		t.Fatal(err)
	}
	if collectedAt != 0 {
		t.Fatalf("collected_at = %d, want 0 once the chat is running again", collectedAt)
	}
}

// A chat that never left Running (mid-turn deltas) must not thrash
// collected_at back and forth — it should simply never have been set for an
// in-flight turn, which UncollectedChildren already guarantees since the
// phase isn't done/failed/stale yet. This just confirms the clear is a no-op
// (not an error) when there is nothing to clear.
func TestCollectedAtClearIsANoOpWhenAlreadyZero(t *testing.T) {
	a := newTestApp(t)
	const chatID = "64"

	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatalf("phase store: %v", err)
	}
	a.phases = store

	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, collected_at) VALUES (64, 1, 'sub-agent', 0)`,
	); err != nil {
		t.Fatal(err)
	}

	a.emitChatLine(chatID, "claude-data", claudeTextLine("m1", "still going"))

	var collectedAt int64
	if err := a.db.QueryRow(`SELECT collected_at FROM chats WHERE id = 64`).Scan(&collectedAt); err != nil {
		t.Fatal(err)
	}
	if collectedAt != 0 {
		t.Fatalf("collected_at = %d, want 0", collectedAt)
	}
}
