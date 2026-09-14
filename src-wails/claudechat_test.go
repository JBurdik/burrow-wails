package main

import (
	"strings"
	"testing"
)

// TestClaudeSendDoesNotRecordAnUndeliveredMessage covers IMPORTANT 3: sending
// to a chat whose CLI is not running (never started, or reaped idle by
// agentproc.Manager.ReapIdle) must not leave the user's line in the
// transcript. ClaudeSend used to call emitChatLine BEFORE claudeWrite, so the
// line was recorded even though claudeMgr().Send always fails with "unknown
// agent session" for an id it has never Start()ed — a permanent, silent data
// loss the caller (chat_send / a waiting parent) had no way to detect.
func TestClaudeSendDoesNotRecordAnUndeliveredMessage(t *testing.T) {
	a := newTestApp(t)
	const chatID = "42"

	err := a.ClaudeSend(chatID, "are you there?", "", nil)
	if err == nil {
		t.Fatal("want an error sending to a chat with no running CLI, got nil")
	}

	// loadFolded (LoadChatMessages), not loadStored: the DB-backed fold is
	// coalesced and would not reflect an emit from this same instant either
	// way, so it can't tell a fix from a debounce window. The in-memory tail
	// is what a live client actually reads, and is authoritative immediately.
	msgs := loadFolded(t, a, 42)
	if len(msgs) != 0 {
		t.Fatalf("want no transcript rows for an undelivered send, got %d: %v", len(msgs), msgs)
	}
}

// TestClaudeSendErrorNamesTheDeadProcess covers the "make the failure
// legible" half of IMPORTANT 3 — a caller driving a sub-agent over chat_send
// must be able to tell "your process isn't running" from any other failure.
func TestClaudeSendErrorNamesTheDeadProcess(t *testing.T) {
	a := newTestApp(t)

	err := a.ClaudeSend("42", "hello", "", nil)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, "42") || !strings.Contains(got, "not running") {
		t.Fatalf("error doesn't name the dead process clearly: %q", got)
	}
}
