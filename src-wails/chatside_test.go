package main

import (
	"slices"
	"testing"
)

func TestChatSideCreateClaudeForksAtLatestAndIsNotASubagent(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "Plan", Transport: "claude-cli",
		ClaudeSessionID: "sess-1", Model: "claude-haiku-x", Effort: "low", PermissionMode: "plan"})
	// The parent is mid-turn: its process is live and must stay untouched.
	if err := a.ChatSend(parent.ID, "long job", nil); err != nil {
		t.Fatal(err)
	}

	side, err := a.ChatSideCreate(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if side.Kind != chatKindSide || side.ParentChatID != parent.ID || side.ClaudeSessionID != "sess-1" ||
		side.Model != "claude-haiku-x" || side.Effort != "low" || side.PermissionMode != "plan" {
		t.Fatalf("side row: %+v", side)
	}
	if msgs, _ := a.chatTranscript(side.ID); len(msgs) != 0 {
		t.Fatalf("a side chat copies no transcript: %+v", msgs)
	}
	// Not collected as a sub-agent's result.
	if ids, _ := a.UncollectedChildren(parent.ID); len(ids) != 0 {
		t.Fatalf("side chat listed as a sub-agent result: %v", ids)
	}
	// A second click before anything was asked reuses the unused row.
	again, err := a.ChatSideCreate(parent.ID)
	if err != nil || again.ID != side.ID {
		t.Fatalf("want reuse of %d, got %d (%v)", side.ID, again.ID, err)
	}

	if err := a.ChatSend(side.ID, "what is X?", nil); err != nil {
		t.Fatal(err)
	}
	args := fake.lastStart().args
	if !hasArgPair(args, "--resume", "sess-1") || !slices.Contains(args, "--fork-session") || slices.Contains(args, "--resume-session-at") {
		t.Fatalf("side start: %v", args)
	}
	if !fake.Alive("1") {
		t.Fatal("parent process was disturbed")
	}
	// The fork reports its own session: from now on the side chat resumes it.
	a.recordChatSessionID("2", "sess-side")
	if at := a.chatResumeAt(side.ID); at != "" {
		t.Fatalf("resume_at not cleared: %q", at)
	}
	if p, _ := a.chatByID(parent.ID); p.ClaudeSessionID != "sess-1" {
		t.Fatalf("parent session changed: %q", p.ClaudeSessionID)
	}
	// Used now: the next click makes a new one.
	next, err := a.ChatSideCreate(parent.ID)
	if err != nil || next.ID == side.ID {
		t.Fatalf("want a new side chat, got %d (%v)", next.ID, err)
	}
	// No side chat of a side chat.
	if _, err := a.ChatSideCreate(side.ID); err == nil {
		t.Fatal("side of a side chat accepted")
	}
}

func TestChatSideCreateCodexForksThread(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{map[string]any{
		"id": "codex", "name": "Codex", "providerId": "codex", "kind": "codex", "transport": "codex-app-server", "enabled": true,
	}}})
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex", ClaudeSessionID: "thr-0"})
	side, err := a.ChatSideCreate(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(side.ID, "quick q", nil); err != nil {
		t.Fatal(err)
	}
	o := fake.rpc.starts[len(fake.rpc.starts)-1]
	if !o.Fork || o.ResumeSessionID != "thr-0" {
		t.Fatalf("codex side start must fork thr-0: %+v", o)
	}
	// An ordinary Codex chat never forks.
	if err := a.ChatSend(parent.ID, "go on", nil); err != nil {
		t.Fatal(err)
	}
	if o := fake.rpc.starts[len(fake.rpc.starts)-1]; o.Fork {
		t.Fatalf("plain start forked: %+v", o)
	}
}

func TestChatSideCreateRefusesACPAndFreshChats(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	acp, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "acp", AgentKind: "gemini", ClaudeSessionID: "s"})
	if _, err := a.ChatSideCreate(acp.ID); err == nil {
		t.Fatal("generic ACP side chat accepted")
	}
	fresh, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if _, err := a.ChatSideCreate(fresh.ID); err == nil {
		t.Fatal("side chat of a chat with no session accepted")
	}
}
