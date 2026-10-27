package main

import (
	"slices"
	"strings"
	"testing"
)

func TestReviewChangesCodexUsesNativeReview(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{
		"providers": []any{map[string]any{"id": "codex", "name": "Codex", "providerId": "codex", "kind": "codex", "transport": "codex-app-server", "enabled": true}},
		"uiPrefs":   map[string]any{"defaultChatAgent": "codex"},
	})
	got, err := a.ReviewChanges(wsID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if got["native"] != true {
		t.Fatalf("want native review: %v", got)
	}
	id := "1"
	if m := fake.rpc.methods(id); !slices.Equal(m, []string{"review/start"}) {
		t.Fatalf("writes: %v", m)
	}
	w := fake.rpc.written(id)
	if !strings.Contains(w, `"uncommittedChanges"`) || !strings.Contains(w, `"delivery":"inline"`) || !strings.Contains(w, `"threadId":"thread-1"`) {
		t.Fatalf("review/start params: %s", w)
	}
	// Against a base branch.
	if _, err := a.ReviewChanges(wsID, 0, "develop"); err != nil {
		t.Fatal(err)
	}
	if w := fake.rpc.written("2"); !strings.Contains(w, `"baseBranch","branch":"develop"`) && !strings.Contains(w, `"branch":"develop"`) {
		t.Fatalf("base target: %s", w)
	}
}

func TestReviewChangesClaudeSendsPromptAsSubagent(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", AgentKind: "claude"})
	got, err := a.ReviewChanges(0, parent.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := a.chatByID(got["chat_id"].(int64))
	if child.ParentChatID != parent.ID || child.WorkspaceID != wsID {
		t.Fatalf("child row: %+v", child)
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0], "git diff main...HEAD") || !strings.Contains(fake.sent[0], "#L") {
		t.Fatalf("prompt: %v", fake.sent)
	}
	// No parent: a top-level chat.
	got, err = a.ReviewChanges(wsID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := a.chatByID(got["chat_id"].(int64)); c.ParentChatID != 0 || !strings.Contains(fake.sent[1], "git diff HEAD") {
		t.Fatalf("top-level review: %+v %v", c, fake.sent)
	}
}

func TestReviewChangesUnknownWorkspace(t *testing.T) {
	a, _, _, _ := runtimeApp(t)
	if _, err := a.ReviewChanges(999, 0, ""); err == nil {
		t.Fatal("want an error")
	}
}
