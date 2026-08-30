package main

import (
	"encoding/json"
	"fmt"
)

// Remote (mobile) read surface.
//
// Everything the phone needs is already on disk: chats and their transcripts
// live in config.json (the desktop's `setConfig` store), workspaces and tabs
// in SQLite. So these commands are plain readers — no new persistence, no
// second source of truth.
//
// Writing is deliberately absent. The desktop rewrites config.json wholesale
// on every setConfig, so a concurrent write from here would be silently
// clobbered on the next save. The desktop stays the single writer; remote
// mutations go through it (see RemoteCreateChat in stubs.go).

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
