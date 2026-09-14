package main

import "testing"

// IMPORTANT 5: `chat_messages` is written by the frontend, but a sub-agent's
// parent workspace can be closed while the child keeps running —
// SubAgentHost.vue only mounts children of OPENED workspaces — so nothing
// ever writes chat_messages for it, even though Go's own `chat_stream` (and
// the phase it derives) keeps advancing with no client attached at all.
// LastAssistantMessage used to read ONLY chat_messages, so this scenario
// returned ("", nil) — a silent, unrecoverable empty "success" that
// wait_result/collect_results both took as a real (if empty) answer and
// marked the child collected.
//
// Simulated here by writing straight into chat_stream (bypassing
// emitChatLine, which would also fold into chat_messages) — the same state a
// process restart with no chat ever adopted into memory would leave behind.
func TestLastAssistantMessageFallsBackToChatStream(t *testing.T) {
	a := newTestApp(t)
	const chatID = "77"

	rows := []struct {
		ord  int64
		kind string
		line string
	}{
		{0, "claude-data", claudeTextLine("m1", "the sub-agent's real answer")},
		{1, "claude-data", claudeResultLine},
	}
	for _, r := range rows {
		if _, err := a.db.Exec(
			`INSERT INTO chat_stream (chat_id, ord, kind, line) VALUES (?, ?, ?, ?)`,
			chatID, r.ord, r.kind, r.line,
		); err != nil {
			t.Fatalf("seed chat_stream: %v", err)
		}
	}

	got, err := a.LastAssistantMessage(77)
	if err != nil {
		t.Fatalf("LastAssistantMessage: %v", err)
	}
	if got != "the sub-agent's real answer" {
		t.Fatalf("got %q, want the chat_stream-derived answer", got)
	}
}

// The failing-closed half: when NEITHER chat_messages nor chat_stream has
// anything, the caller must see an explicit error, not an empty success —
// wait_result/collect_results only skip MarkCollected when LastAssistantMessage
// errors.
func TestLastAssistantMessageErrorsRatherThanReturningEmpty(t *testing.T) {
	a := newTestApp(t)

	_, err := a.LastAssistantMessage(999)
	if err == nil {
		t.Fatal("want an explicit error for a chat with no transcript anywhere, got nil")
	}
}

// The ordinary path — chat_messages populated, as the frontend does it — must
// keep working unchanged; the fallback must never shadow a real answer.
func TestLastAssistantMessagePrefersChatMessagesOverChatStream(t *testing.T) {
	a := newTestApp(t)
	const chatID = 55

	if err := a.SaveChatMessages(chatID, `[{"id":1,"role":"assistant","text":"from chat_messages"}]`, -1); err != nil {
		t.Fatalf("save: %v", err)
	}
	// A stale/different chat_stream row must not win over the real transcript.
	if _, err := a.db.Exec(
		`INSERT INTO chat_stream (chat_id, ord, kind, line) VALUES (?, 0, 'claude-data', ?)`,
		"55", claudeTextLine("m1", "from chat_stream — should be ignored"),
	); err != nil {
		t.Fatalf("seed chat_stream: %v", err)
	}

	got, err := a.LastAssistantMessage(chatID)
	if err != nil {
		t.Fatalf("LastAssistantMessage: %v", err)
	}
	if got != "from chat_messages" {
		t.Fatalf("got %q, want the chat_messages answer to win", got)
	}
}
