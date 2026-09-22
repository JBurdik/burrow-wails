package main

import (
	"math"
	"testing"
)

func TestEstimateCostUSDPreservesModelVersionBoundaries(t *testing.T) {
	tests := []struct {
		model  string
		want   float64
		priced bool
	}{
		{" ANTHROPIC/claude-opus-4.1-thinking ", 15, true},
		{"claude-opus-4.10", 5, true},
		{"claude-opus-4-20250514", 15, true},
		{"claude-opus-4.20250514", 15, true},
		{"claude.opus.4.9", 0, false},
		{"claude-opus-4.9", 5, true},
		{"claude-sonnet-50", 0, false},
		{"claude-sonnet-5-thinking", 2, true},
		{"claude-sonnet-5-opus-5", 5, true},
		{"claude-3.5-sonnet-20241022", 3, true},
	}
	for _, tt := range tests {
		got, priced := estimateCostUSD(tt.model, 1_000_000, 0, 0, 0)
		if priced != tt.priced || math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("estimateCostUSD(%q) = (%v, %v), want (%v, %v)", tt.model, got, priced, tt.want, tt.priced)
		}
	}
}

func TestEstimateCostUSDAccountsForCacheReadsAndWrites(t *testing.T) {
	got, priced := estimateCostUSD("claude-sonnet-5", 0, 0, 1_000_000, 1_000_000)
	if !priced || math.Abs(got-2.7) > 1e-9 {
		t.Fatalf("cache estimate = (%v, %v), want (2.7, true)", got, priced)
	}
}

func TestGetChatUsageScansEachStreamLineOnlyOnce(t *testing.T) {
	a := newTestApp(t)
	chat, err := a.CreateChat(Chat{WorkspaceID: 1, Model: "claude-sonnet-5"})
	if err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","message":{"usage":{"input_tokens":12,"output_tokens":34,"cache_read_input_tokens":56,"cache_creation_input_tokens":78},"content":[]}}`
	if _, err := a.db.Exec(`INSERT INTO chat_stream (chat_id, ord, kind, line) VALUES (?, 0, 'claude-data', ?)`, chat.ID, line); err != nil {
		t.Fatal(err)
	}
	first, err := a.GetChatUsage()
	if err != nil {
		t.Fatal(err)
	}
	// A fresh App instance is the restart boundary: it has no in-memory usage
	// state, so the persisted scanned_ord and totals must carry the result.
	fresh := &App{db: a.db}
	second, err := fresh.GetChatUsage()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Chats) != 1 || len(second.Chats) != 1 || first.Chats[0].InputTokens != 12 || second.Chats[0].InputTokens != 12 || second.Chats[0].OutputTokens != 34 || second.Chats[0].CacheReadTokens != 56 || second.Chats[0].CacheCreationTokens != 78 {
		t.Fatalf("usage was not stable across scans: first=%+v second=%+v", first, second)
	}
}
