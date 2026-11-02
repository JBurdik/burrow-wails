package main

import (
	"slices"
	"strconv"
	"testing"
)

// The fold keeps the latest Claude record uuid on each message — the resume
// point a rewind or fork hands --resume-session-at.
func TestFoldStampsClaudeRecordUUID(t *testing.T) {
	var events []ProviderRuntimeEvent
	for _, line := range []string{
		`{"type":"assistant","uuid":"u-think","message":{"id":"m1","content":[{"type":"thinking","thinking":"hmm"}]}}`,
		`{"type":"assistant","uuid":"u-text1","message":{"id":"m1","content":[{"type":"text","text":"Let me look. "}]}}`,
		`{"type":"assistant","uuid":"u-tool","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
		`{"type":"user","uuid":"u-result","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","uuid":"u-text2","message":{"id":"m2","content":[{"type":"text","text":"Done"}]}}`,
		`{"type":"assistant","uuid":"u-text3","message":{"id":"m2","content":[{"type":"text","text":"!"}]}}`,
	} {
		events = append(events, NormalizeClaudeStreamLine(line)...)
	}
	msgs := foldEvents(events)
	var got []string
	for _, m := range msgs {
		got = append(got, m.Role+":"+m.UUID)
	}
	want := []string{"thinking:u-think", "assistant:u-text1", "tool:u-result", "assistant:u-text3"}
	if !slices.Equal(got, want) {
		t.Fatalf("uuids = %v, want %v", got, want)
	}
}

func rewindFixture() []ChatMessage {
	return []ChatMessage{
		{ID: 0, Role: "user", Text: "a"},
		{ID: 1, Role: "thinking", Text: "…", UUID: "u0"},
		{ID: 2, Role: "assistant", Text: "A", UUID: "u1"},
		{ID: 3, Role: "user", Text: "b"},
		{ID: 4, Role: "tool", Text: "Bash", UUID: "u2"},
		{ID: 5, Role: "assistant", Text: "B", UUID: "u3"},
		{ID: 6, Role: "user", Text: "c"},
	}
}

func TestChatCutAndResumePoint(t *testing.T) {
	msgs := rewindFixture()
	cases := []struct {
		name     string
		index    int
		text     string
		fork     bool
		keep     int
		resumeAt string
		wantErr  bool
	}{
		{"rewind first prompt starts fresh", 0, "a", false, 0, "", false},
		{"rewind second prompt resumes after first turn", 3, "b", false, 3, "u1", false},
		{"rewind third prompt", 6, "c", false, 6, "u3", false},
		{"rewind refuses a reply", 2, "A", false, 0, "", true},
		{"stale text refused", 3, "x", false, 0, "", true},
		{"out of range refused", 9, "", false, 0, "", true},
		{"fork prompt keeps its turn", 3, "b", true, 6, "u3", false},
		{"fork reply keeps its turn", 2, "A", true, 3, "u1", false},
		// The last prompt has no answer yet: the fork resumes at the turn before.
		{"fork unanswered prompt", 6, "c", true, 7, "u3", false},
		{"fork refuses a tool row", 4, "Bash", true, 0, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep, err := chatCut(msgs, tc.index, tc.text, tc.fork)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got keep=%d", keep)
				}
				return
			}
			if err != nil || keep != tc.keep {
				t.Fatalf("keep = %d, %v; want %d", keep, err, tc.keep)
			}
			at, err := chatResumePoint(msgs[:keep])
			if err != nil || at != tc.resumeAt {
				t.Fatalf("resume = %q, %v; want %q", at, err, tc.resumeAt)
			}
		})
	}
}

// An older transcript (no uuids) must refuse rather than resume somewhere
// that silently drops a turn the transcript still shows.
func TestChatResumePointRefusesMissingUUID(t *testing.T) {
	keep := []ChatMessage{
		{Role: "user", Text: "a"}, {Role: "assistant", Text: "A", UUID: "u1"},
		{Role: "user", Text: "b"}, {Role: "assistant", Text: "B"},
	}
	if at, err := chatResumePoint(keep); err == nil {
		t.Fatalf("want error, got %q", at)
	}
}

func saveFixture(t *testing.T, a *App, chatID int64, msgs []ChatMessage) {
	t.Helper()
	if err := a.truncateChatTranscript(strconv.FormatInt(chatID, 10), msgs); err != nil {
		t.Fatal(err)
	}
}

func TestChatRewindTruncatesAndResumesForked(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1"})
	saveFixture(t, a, c.ID, rewindFixture())
	w := a.chatStream()
	for i := 0; i < 3; i++ {
		w.append("1", "claude-data", `{"type":"system"}`)
	}
	drainChatStream(t, a, "1")
	events := chatEvents(t, "1")
	_ = a.ChatSend(c.ID, "hi", nil)

	if err := a.ChatRewind(c.ID, 3, "b"); err != nil {
		t.Fatal(err)
	}
	if fake.Alive("1") {
		t.Fatal("rewind must stop the process")
	}
	msgs, _ := a.chatTranscript(c.ID)
	if len(msgs) != 3 || msgs[2].UUID != "u1" {
		t.Fatalf("transcript after rewind: %+v", msgs)
	}
	if at := a.chatResumeAt(c.ID); at != "u1" {
		t.Fatalf("resume_at = %q", at)
	}
	row, _ := a.chatByID(c.ID)
	if row.ClaudeSessionID != "sess-1" || row.MessageCount != 3 {
		t.Fatalf("row after rewind: %+v", row)
	}
	// Every line recorded before the cut is gone — only the reset marker
	// written after it remains — and the marker is past them all, so neither
	// a replay nor an adoption can fold them back.
	drainChatStream(t, a, "1")
	folded, _ := a.ChatFoldedOrd("1")
	var left int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_stream WHERE chat_id = '1' AND kind != ?`, chatRuntimeKind).Scan(&left)
	if left != 0 || folded < 5 {
		t.Fatalf("stream not truncated: %d old rows left, folded_ord %d", left, folded)
	}
	got := events()
	if len(got) == 0 || got[len(got)-1].Type != EvtSessionReset || got[len(got)-1].Cleared {
		t.Fatalf("want a trailing session.reset, got %+v", got)
	}

	_ = a.ChatSend(c.ID, "b2", nil)
	args := fake.lastStart().args
	i := slices.Index(args, "--resume")
	if i < 0 || args[i+1] != "sess-1" || !slices.Contains(args, "--fork-session") ||
		args[slices.Index(args, "--resume-session-at")+1] != "u1" {
		t.Fatalf("start after rewind: %v", args)
	}
	// The forked session reporting its id consumes the resume point.
	a.recordChatSessionID("1", "sess-2")
	if at := a.chatResumeAt(c.ID); at != "" {
		t.Fatalf("resume_at not cleared: %q", at)
	}
}

func TestChatRewindToFirstPromptStartsFresh(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1"})
	saveFixture(t, a, c.ID, rewindFixture())
	if err := a.ChatRewind(c.ID, 0, "a"); err != nil {
		t.Fatal(err)
	}
	if row, _ := a.chatByID(c.ID); row.ClaudeSessionID != "" {
		t.Fatalf("session kept: %+v", row)
	}
	_ = a.ChatSend(c.ID, "a2", nil)
	if slices.Contains(fake.lastStart().args, "--resume") {
		t.Fatalf("fresh start resumed: %v", fake.lastStart().args)
	}
}

func TestChatForkCopiesUpToTheCutAndLeavesParent(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "Plan", Transport: "claude-cli",
		ClaudeSessionID: "sess-1", Model: "claude-haiku-x", Effort: "low", PermissionMode: "plan"})
	saveFixture(t, a, parent.ID, rewindFixture())

	child, err := a.ChatFork(parent.ID, 0, "a")
	if err != nil {
		t.Fatal(err)
	}
	if child.Title != "Plan (fork)" || child.WorkspaceID != wsID || child.ClaudeSessionID != "sess-1" ||
		child.Model != "claude-haiku-x" || child.Effort != "low" || child.PermissionMode != "plan" || child.ParentChatID != 0 {
		t.Fatalf("fork row: %+v", child)
	}
	msgs, _ := a.chatTranscript(child.ID)
	if len(msgs) != 3 || msgs[2].Text != "A" {
		t.Fatalf("fork transcript: %+v", msgs)
	}
	if pm, _ := a.chatTranscript(parent.ID); len(pm) != len(rewindFixture()) {
		t.Fatalf("parent transcript changed: %d rows", len(pm))
	}
	if at := a.chatResumeAt(parent.ID); at != "" {
		t.Fatalf("parent got a resume point: %q", at)
	}

	_ = a.ChatSend(child.ID, "next", nil)
	args := fake.lastStart().args
	if args[slices.Index(args, "--resume")+1] != "sess-1" || args[slices.Index(args, "--resume-session-at")+1] != "u1" ||
		!slices.Contains(args, "--fork-session") {
		t.Fatalf("fork start: %v", args)
	}
}

func TestChatRewindAndForkRefuseNonClaude(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, AgentKind: "codex", Transport: "codex-app-server"})
	if err := a.ChatRewind(c.ID, 0, "a"); err == nil {
		t.Fatal("codex rewind must be refused")
	}
	if _, err := a.ChatFork(c.ID, 0, "a"); err == nil {
		t.Fatal("codex fork must be refused")
	}
}
