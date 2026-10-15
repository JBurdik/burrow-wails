package main

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"burrow/internal/control"
)

func TestSpawnSubagentRunsInGo(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{
		"providers": []any{claudeAt("claude", ""), map[string]any{"id": "codex", "name": "Codex", "providerId": "codex", "kind": "codex", "transport": "codex-app-server", "enabled": true}},
		// A saved override of the scout profile's model.
		"subagentProfiles": []any{map[string]any{"id": "scout", "model": "claude-haiku-x", "permissionMode": "plan"}},
	})
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", AgentKind: "claude",
		Model: "claude-opus-x", Effort: "max", PermissionMode: "acceptEdits"})

	// No profile: the parent's launch settings.
	got, err := a.spawnSubagent(control.SpawnRequest{Task: "look around\nmore", ParentChatID: parent.ID})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	child, _ := a.chatByID(got.ChatID)
	if child.ParentChatID != parent.ID || child.Model != "claude-opus-x" || child.Effort != "max" ||
		child.PermissionMode != "acceptEdits" || child.ProfileID != "claude" || child.Title != "look around" {
		t.Fatalf("child row: %+v", child)
	}
	if fake.startCount() != 1 || !strings.Contains(fake.sent[0], "look around") {
		t.Fatalf("child not started with its task: %d %v", fake.startCount(), fake.sent)
	}

	// Profile: its permission and saved model win, the task is wrapped.
	got, err = a.spawnSubagent(control.SpawnRequest{Task: "find it", Profile: "Scout", ParentChatID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	child, _ = a.chatByID(got.ChatID)
	if child.PermissionMode != "plan" || child.Model != "claude-haiku-x" || !strings.HasPrefix(got.Task, "Role: Scout") {
		t.Fatalf("profile not applied: %+v task=%q", child, got.Task)
	}

	// A Codex child runs in the runtime too, and does not inherit a Claude
	// parent's model / effort / mode — they mean nothing to Codex.
	got, err = a.spawnSubagent(control.SpawnRequest{Task: "x", Agent: "codex", ParentChatID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	child, _ = a.chatByID(got.ChatID)
	if child.Transport != "codex-app-server" || child.Model != "" || child.Effort != "" || child.PermissionMode != "" {
		t.Fatalf("codex child row: %+v", child)
	}
	if len(fake.rpc.starts) != 1 || !slices.Contains(fake.rpc.methods(strconv.FormatInt(got.ChatID, 10)), "turn/start") {
		t.Fatalf("codex child not started with its task: %v", fake.rpc.starts)
	}
	if _, err := a.spawnSubagent(control.SpawnRequest{Task: "x", Agent: "nope", ParentChatID: parent.ID}); err == nil {
		t.Fatal("unknown agent must be an error")
	}
}

// The default instructions are duplicated in src/lib/subagentProfiles.ts;
// a spawn from Go and one from the UI must hand the agent the same role text.
func TestSubagentProfileDefaultsMatchFrontend(t *testing.T) {
	src, err := os.ReadFile("../src/lib/subagentProfiles.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range defaultSubagentProfiles() {
		if !strings.Contains(string(src), p.Instructions) || !strings.Contains(string(src), `permissionMode: "`+p.PermissionMode+`"`) {
			t.Fatalf("profile %s differs from subagentProfiles.ts", p.ID)
		}
	}
}

func TestSubagentNoteFoldsIntoParentRow(t *testing.T) {
	events := NormalizeChatLine(chatNoteKind, subagentNote(42, "  fix the tests\nmore detail", "Claude"), 7)
	got := foldEvents(events)
	if len(got) != 1 {
		t.Fatalf("want one row, got %+v", got)
	}
	m := got[0]
	if m.Role != "system-info" || m.Text != "Spawned sub-agent: fix the tests" || m.SubagentChatID != 42 || m.SubagentAgent != "Claude" {
		t.Fatalf("unexpected row %+v", m)
	}
	if got := foldEvents(NormalizeChatLine(chatNoteKind, subagentNote(1, "\n", "x"), 0)); len(got) != 1 || got[0].Text != "Spawned Sub-agent" {
		t.Fatalf("empty task label: %+v", got)
	}
}
