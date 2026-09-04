package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The phone renders whatever this returns, so the contract is the shape in
// src/mobile/store.ts: camelCase session keys plus an always-present
// `messages` array pulled from the separate history map.
func TestRemoteChatsFromConfig(t *testing.T) {
	raw := `{
	  "chatSessions": [
	    {"id": 8, "workspaceId": 2, "title": "Chat 8", "busy": false, "transport": "claude-cli", "claudeSessionId": "abc"},
	    {"id": 9, "workspaceId": 2, "title": "Chat 9", "busy": true,  "transport": "acp",        "claudeSessionId": ""},
	    {"id": 99, "workspaceId": 2, "title": "Manager", "control": true, "transport": "claude-cli"}
	  ],
	  "chatMessageHistory": {
	    "8": [{"id": 0, "role": "user", "text": "ahoj"}, {"id": 1, "role": "assistant", "text": "čau"}]
	  }
	}`
	chats, err := remoteChatsFromConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 {
		t.Fatalf("got %d chats, want 2 — the control:true Manager session must be hidden", len(chats))
	}
	msgs, ok := chats[0]["messages"].([]map[string]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("chat 8 messages: %#v", chats[0]["messages"])
	}
	if msgs[1]["text"] != "čau" {
		t.Fatalf("transcript not inlined: %#v", msgs[1])
	}
	// A chat with no history must still carry an array, never null — store.ts
	// only guards with Array.isArray, and null would blank the transcript.
	empty, ok := chats[1]["messages"].([]map[string]any)
	if !ok || len(empty) != 0 {
		t.Fatalf("chat 9 messages: %#v", chats[1]["messages"])
	}
	if b, _ := json.Marshal(chats[1]["messages"]); string(b) != "[]" {
		t.Fatalf("empty transcript must marshal to [], got %s", b)
	}
}

func TestRemoteChatsFromConfigTolerantOfEmpty(t *testing.T) {
	for _, raw := range []string{`{}`, `{"chatSessions": []}`} {
		chats, err := remoteChatsFromConfig(raw)
		if err != nil || len(chats) != 0 {
			t.Fatalf("%s: got %v / %v", raw, chats, err)
		}
	}
	if _, err := remoteChatsFromConfig("not json"); err == nil {
		t.Fatal("expected a parse error")
	}
}

// Guards against drift in the real file: whatever the desktop has actually
// written must still parse. Skipped when there is no config yet.
func TestRemoteChatsFromLiveConfig(t *testing.T) {
	dir, err := appDataDir()
	if err != nil {
		t.Skip(err)
	}
	raw, err := os.ReadFile(dir + "/config.json")
	if err != nil {
		t.Skip("no config.json on this machine")
	}
	if _, err := remoteChatsFromConfig(string(raw)); err != nil {
		t.Fatalf("live config.json no longer parses: %v", err)
	}
}

func TestRemoteCreateChatSessionBumpsCounterAndPreservesConfig(t *testing.T) {
	cfg := map[string]any{
		"someUnrelatedSetting": "keep-me",
		"chatIdCounter":        float64(6),
		"chatSessions": []any{
			map[string]any{"id": float64(5), "workspaceId": float64(2), "title": "Chat 1"},
		},
	}
	session, id := remoteCreateChatSession(cfg, 2, "claude")

	if id != 6 {
		t.Fatalf("id = %d, want 6 (must bump the counter, not reuse the last id)", id)
	}
	if cfg["chatIdCounter"] != float64(7) {
		t.Fatalf("chatIdCounter = %v, want 7", cfg["chatIdCounter"])
	}
	if cfg["someUnrelatedSetting"] != "keep-me" {
		t.Fatal("unrelated config keys must survive — config.json is a grab-bag, not just chat state")
	}
	if session["title"] != "Chat 2" {
		t.Fatalf("title = %v, want \"Chat 2\" (second chat for this workspace)", session["title"])
	}
	if session["transport"] != "claude-cli" {
		t.Fatalf("transport = %v, want claude-cli", session["transport"])
	}
	sessions, ok := cfg["chatSessions"].([]any)
	if !ok || len(sessions) != 2 {
		t.Fatalf("chatSessions = %#v, want 2 entries", cfg["chatSessions"])
	}
	history, ok := cfg["chatMessageHistory"].(map[string]any)
	if !ok {
		t.Fatal("chatMessageHistory was not created")
	}
	if msgs, ok := history["6"].([]any); !ok || len(msgs) != 0 {
		t.Fatalf("chatMessageHistory[6] = %#v, want []", history["6"])
	}
}

func TestRemoteCreateChatRejectsNonClaude(t *testing.T) {
	a := &App{}
	if _, err := a.RemoteCreateChat(1, "codex"); err == nil {
		t.Fatal("expected an error — remote chat creation only supports agentKind claude for now")
	}
}

// RemoteCreateChat itself can't be exercised end-to-end here with an unknown
// workspace id: resolving it goes through workspaceLabels() ->
// ListWorkspaces(), which calls a.db.Query on the real *sql.DB — a bare
// &App{} has a nil db, and database/sql panics (nil pointer dereference in
// DB.conn) rather than returning an error. That's a pre-existing nil-DB
// safety gap in ListWorkspaces/workspaceLabels, unrelated to this fix, and
// out of scope here. So this tests the pure resolution logic RemoteCreateChat
// added instead, which needs no DB at all.
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
