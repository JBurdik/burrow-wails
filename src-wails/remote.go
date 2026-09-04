package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// Remote (mobile) read surface.
//
// Everything the phone needs is already on disk: chats and their transcripts
// live in config.json (the desktop's `setConfig` store), workspaces and tabs
// in SQLite. So these commands are plain readers — no new persistence, no
// second source of truth.
//
// Writing is deliberately rare. The desktop rewrites config.json wholesale
// on every setConfig, so a concurrent write from here can be clobbered on
// the next save — RemoteCreateChat below accepts that risk for the one
// mutation the phone needs (see its comment for why).

// remoteConfig is the slice of config.json the phone cares about. Keys match
// claudeChats.ts's SESSIONS_KEY / HISTORY_KEY exactly.
type remoteConfig struct {
	ChatSessions       []map[string]any            `json:"chatSessions"`
	ChatMessageHistory map[string][]map[string]any `json:"chatMessageHistory"`
}

// RemoteListChats returns every chat with its transcript inlined, in the shape
// src/mobile/store.ts's RemoteChat expects. The persisted keys are already
// camelCase, so the session objects pass through untouched.
func (a *App) RemoteListChats() ([]map[string]any, error) {
	raw, err := a.ReadConfig()
	if err != nil {
		return nil, err
	}
	chats, err := remoteChatsFromConfig(raw)
	if err != nil {
		return nil, err
	}
	// config.json stores only workspaceId. Resolve the name here so the phone
	// can label a chat without having loaded the workspace list first.
	names, paths := a.workspaceLabels()
	for _, chat := range chats {
		id, _ := numericID(chat["workspaceId"])
		chat["workspaceName"] = names[id]
		chat["workspacePath"] = paths[id]
	}
	return chats, nil
}

func (a *App) workspaceLabels() (map[int64]string, map[int64]string) {
	names, paths := map[int64]string{}, map[int64]string{}
	wss, err := a.ListWorkspaces()
	if err != nil {
		return names, paths
	}
	for _, w := range wss {
		names[w.ID] = w.Name
		paths[w.ID] = w.Path
	}
	return names, paths
}

// remoteChatsFromConfig is the pure half, so the shape contract with
// src/mobile/store.ts can be tested without an app data dir.
func remoteChatsFromConfig(raw string) ([]map[string]any, error) {
	var cfg remoteConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}
	out := make([]map[string]any, 0, len(cfg.ChatSessions))
	for _, chat := range cfg.ChatSessions {
		// Mission Control's hidden session is not a user-facing chat — the
		// desktop sidebar hides it for the same reason.
		if ctrl, ok := chat["control"].(bool); ok && ctrl {
			continue
		}
		id, ok := numericID(chat["id"])
		if !ok {
			continue
		}
		msgs := cfg.ChatMessageHistory[fmt.Sprint(id)]
		if msgs == nil {
			msgs = []map[string]any{}
		}
		chat["messages"] = msgs
		out = append(out, chat)
	}
	return out, nil
}

// remoteCreateChatSession mutates cfg (config.json already decoded into a
// generic map — NOT the narrow remoteConfig struct, which would drop every
// other settings key on write-back) in place: bumps chatIdCounter, appends a
// new session row in the exact shape src/stores/claudeChats.ts#create()
// builds client-side, and seeds an empty transcript. Pure function so the
// id-allocation and shape logic is testable without a real app data dir.
func remoteCreateChatSession(cfg map[string]any, workspaceID int64, agentKind string) (map[string]any, int64) {
	// claudeChats.ts's nextId is post-increment (`const id = nextId++`), so
	// the persisted counter always equals (max used id) + 1 — mirror that
	// invariant exactly rather than reserving extra headroom against a race
	// that RemoteCreateChat's own doc comment already accepts as-is.
	counter := int64(1)
	if v, ok := cfg["chatIdCounter"].(float64); ok {
		counter = int64(v)
	}
	id := counter
	cfg["chatIdCounter"] = float64(id + 1)

	sessions, _ := cfg["chatSessions"].([]any)
	countForWs := 0
	for _, raw := range sessions {
		if s, ok := raw.(map[string]any); ok {
			if wsID, ok := numericID(s["workspaceId"]); ok && wsID == workspaceID {
				countForWs++
			}
		}
	}

	transport := "claude-cli"
	if agentKind != "claude" {
		transport = "acp"
	}
	session := map[string]any{
		"id":              float64(id),
		"workspaceId":     float64(workspaceID),
		"claudeSessionId": "",
		"title":           fmt.Sprintf("Chat %d", countForWs+1),
		"busy":            false,
		"messageCount":    0,
		"agentKind":       agentKind,
		"transport":       transport,
		"lastActivityAt":  float64(time.Now().UnixMilli()),
	}
	cfg["chatSessions"] = append(sessions, session)

	history, _ := cfg["chatMessageHistory"].(map[string]any)
	if history == nil {
		history = map[string]any{}
	}
	history[fmt.Sprint(id)] = []any{}
	cfg["chatMessageHistory"] = history

	return session, id
}

// RemoteCreateChat is the one write RemoteListChats's read-only comment
// above deliberately excluded — see that comment for why config.json's
// read-modify-write here can race a concurrent desktop setConfig save. The
// window is one HTTP round trip right after the phone picks a workspace, so
// the risk is accepted rather than adding cross-process locking for it.
//
// Claude-only for now: an ACP/Codex session needs command/args/configDir
// resolved from provider config that today only exists in
// AgentChat.vue's acpStartPayload() — porting that is future work, not
// wired here.
func (a *App) RemoteCreateChat(workspaceID int64, agentKind string) (map[string]any, error) {
	if agentKind != "claude" {
		return nil, fmt.Errorf("remote chat creation only supports Claude for now (got %q)", agentKind)
	}

	raw, err := a.ReadConfig()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}

	session, id := remoteCreateChatSession(cfg, workspaceID, agentKind)

	out, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := a.WriteConfig(string(out)); err != nil {
		return nil, err
	}

	names, paths := a.workspaceLabels()
	cwd := paths[workspaceID]
	chatID := fmt.Sprint(id)
	if err := a.ClaudeStart(chatID, cwd, "", "default", "", "", "", "", "", ""); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	session["messages"] = []map[string]any{}
	session["workspaceName"] = names[workspaceID]
	session["workspacePath"] = cwd
	return session, nil
}

// numericID copes with JSON numbers arriving as float64.
func numericID(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}
