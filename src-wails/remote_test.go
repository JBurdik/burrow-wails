package main

import (
	"encoding/json"
	"testing"
)

// The config.json-parsing tests that used to live here are gone with the code
// they covered (remoteChatsFromConfig, remoteCreateChatSession and its
// chatIdCounter bump). The chat list is a SQLite table now — chats_test.go
// covers the store, and what is left here is the SHAPING this file still owns.

func TestRemoteChatShapeCarriesWhatThePhoneReads(t *testing.T) {
	names := map[int64]string{2: "Burrow GO"}
	paths := map[int64]string{2: "/repo/path"}
	got := remoteChatShape(Chat{
		ID: 86, WorkspaceID: 2, Title: "Chat 56 (phone)",
		AgentKind: "claude", Transport: "claude-cli", MessageCount: 3,
		LastActivityAt: 1234,
	}, names, paths)

	for key, want := range map[string]any{
		"id":             int64(86),
		"workspaceId":    int64(2),
		"title":          "Chat 56 (phone)",
		"transport":      "claude-cli",
		"agentKind":      "claude",
		"messageCount":   int64(3),
		"lastActivityAt": int64(1234),
		"workspaceName":  "Burrow GO",
		"workspacePath":  "/repo/path",
		"busy":           false,
	} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	// The phone indexes this without a guard and fills it from the event
	// stream; a missing key is a crash on first paint.
	if _, ok := got["messages"]; !ok {
		t.Error("messages missing")
	}
}

func TestRemoteChatShapeDefaultsAnEmptyTransport(t *testing.T) {
	// The phone picks its permission channel off `transport`
	// (claude-data-* vs acp-req-*), so an empty value routes a Claude chat's
	// approval requests to a channel nothing publishes on — the prompt simply
	// never appears and the turn hangs.
	got := remoteChatShape(Chat{ID: 1, WorkspaceID: 1}, nil, nil)
	if got["transport"] != "claude-cli" {
		t.Fatalf("transport = %#v, want claude-cli", got["transport"])
	}
}

func TestRemoteListChatsHidesTheManagerSession(t *testing.T) {
	// Mission Control's session is not a user-facing chat; the desktop
	// sidebar hides it for the same reason.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	if _, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "real"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "Manager", Control: true}); err != nil {
		t.Fatal(err)
	}

	list, err := a.RemoteListChats()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["title"] != "real" {
		t.Fatalf("want only the user-facing chat, got %+v", list)
	}
}

func TestRemoteCreateChatRejectsUnsupportedAgentKind(t *testing.T) {
	a := &App{}
	if _, err := a.RemoteCreateChat(1, "gemini", "", "", ""); err == nil {
		t.Fatal("expected an error — remote chat creation only supports agentKind claude and codex")
	}
}

func TestResolveWorkspaceCwdRejectsUnknownWorkspace(t *testing.T) {
	paths := map[int64]string{2: "/repo/path"}
	if _, err := resolveWorkspaceCwd(paths, 999999); err == nil {
		t.Fatal("expected an error for an unknown workspace id")
	}
	if _, err := resolveWorkspaceCwd(map[int64]string{2: ""}, 2); err == nil {
		t.Fatal("expected an error for a workspace resolved to an empty path")
	}
	cwd, err := resolveWorkspaceCwd(paths, 2)
	if err != nil || cwd != "/repo/path" {
		t.Fatalf("cwd = %q, err = %v, want \"/repo/path\", nil", cwd, err)
	}
}

// Empty SQL results used to marshal as JSON null, which threw inside
// store.ts's `tabs.filter(...)` and surfaced on the phone as "Relace se
// nepodařilo načíst". Every list the frontend maps over must be [].
func TestEmptyListsMarshalAsArrays(t *testing.T) {
	for name, v := range map[string]any{
		"terminal tabs":   []TerminalTab{},
		"workspaces":      []Workspace{},
		"claude sessions": []ClaudeSessionInfo{},
		"skills":          []SkillInfo{},
		"control verbs":   []ControlVerb{},
		"pty sessions":    []string{},
		"chats":           []Chat{},
	} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(b) != "[]" {
			t.Errorf("%s marshalled to %s, want []", name, b)
		}
	}
}
