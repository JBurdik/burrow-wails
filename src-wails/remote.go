package main

import (
	"fmt"
	"strings"
	"time"
)

// The phone's chat surface.
//
// It used to read and write the chat list in config.json, and its own doc
// comments carried the caveat that a concurrent desktop `setConfig` could
// clobber anything written here. That caveat is gone: the list is a SQLite
// table Go owns (chats.go), so this file is now a thin shaping layer — take
// a Chat row, dress it in the camelCase keys src/mobile/store.ts's RemoteChat
// expects, and hand it over.
//
// The one thing still shaped rather than stored is `messages`: the phone
// fills a transcript from the `chat-event-*` stream, so the list hands it an
// empty array to index into rather than a body. Transcripts live in
// chat_stream and are replayed from `folded_ord` — a second copy in this
// reply would be a second replay log for the same data.

// RemoteListChats returns every user-facing chat in the shape
// src/mobile/store.ts expects.
func (a *App) RemoteListChats() ([]map[string]any, error) {
	chats, err := a.ListChats()
	if err != nil {
		return nil, err
	}
	// The table stores only workspace_id. Resolve the label here so the phone
	// can name a chat without having loaded the workspace list first.
	names, paths := a.workspaceLabels()

	out := make([]map[string]any, 0, len(chats))
	for _, c := range chats {
		out = append(out, remoteChatShape(c, names, paths))
	}
	return out, nil
}

// remoteChatShape maps one row onto the phone's camelCase keys. Split out as a
// pure function because it is the seam where the Go column names and the
// client's field names meet, and a silent mismatch there shows up as a chat
// with no title rather than as an error.
func remoteChatShape(c Chat, names, paths map[int64]string) map[string]any {
	transport := c.Transport
	if transport == "" {
		// The phone picks its permission channel off this (`claude-data-*` vs
		// `acp-req-*`), so an empty value would route a Claude chat's approval
		// requests to a channel nothing publishes on and the prompt would
		// never appear. Default to the only transport RemoteCreateChat makes.
		transport = "claude-cli"
	}
	return map[string]any{
		"id":              c.ID,
		"workspaceId":     c.WorkspaceID,
		"title":           c.Title,
		"claudeSessionId": c.ClaudeSessionID,
		// Always false: whether a turn is running is the phase, which the
		// phone learns from the event stream. A value here would be a stale
		// snapshot of it.
		"busy":           false,
		"messageCount":   c.MessageCount,
		"agentKind":      c.AgentKind,
		"transport":      transport,
		"model":          c.Model,
		"archivedAt":     c.ArchivedAt,
		"lastActivityAt": c.LastActivityAt,
		"workspaceName":  names[c.WorkspaceID],
		"workspacePath":  paths[c.WorkspaceID],
		"messages":       []map[string]any{},
	}
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

// resolveWorkspaceCwd fails closed on an unknown workspace id — a request
// naming a workspace the app doesn't know about must error instead of
// silently resolving to cwd="" and letting ClaudeStart spawn the CLI in an
// empty working directory.
func resolveWorkspaceCwd(paths map[int64]string, workspaceID int64) (string, error) {
	cwd, ok := paths[workspaceID]
	if !ok || cwd == "" {
		return "", fmt.Errorf("unknown workspace %d", workspaceID)
	}
	return cwd, nil
}

// RemoteCreateChat creates a chat: a row with its launch settings and nothing
// else. Its process starts on the first chat_send (chatruntime.go), from the
// row, for Claude and Codex alike.
//
// The long warning that used to sit here is obsolete: the id comes from the
// database, so it cannot be reissued to somebody else, and no other client's
// save can revert this row (see chats.go). `remoteCreateMu` went with it —
// it only ever serialized two RemoteCreateChat calls against a hazard the
// database now owns, and its own comment admitted it never closed the real
// one.
//
// Claude and Codex only — the two providers the phone's composer offers.
//
// model/effort/permissionMode come straight from WelcomeView's composer.
// codexModeSettings (acp.go) aliases Claude's own permissionMode ids, so the
// SAME composer value works for both agents.
func (a *App) RemoteCreateChat(workspaceID int64, agentKind, model, effort, permissionMode string) (map[string]any, error) {
	if agentKind != "claude" && agentKind != "codex" {
		return nil, fmt.Errorf("remote chat creation only supports claude and codex (got %q)", agentKind)
	}

	// Resolve and validate the workspace BEFORE creating anything, so an
	// invalid request never leaves a row behind.
	names, paths := a.workspaceLabels()
	if _, err := resolveWorkspaceCwd(paths, workspaceID); err != nil {
		return nil, err
	}

	existing, err := a.ListChats()
	if err != nil {
		return nil, err
	}
	countForWs := 0
	for _, c := range existing {
		if c.WorkspaceID == workspaceID {
			countForWs++
		}
	}

	transport := "claude-cli"
	if agentKind == "codex" {
		transport = "codex-app-server"
		// Checked here rather than at the first send: Codex starts every thread
		// in Auto, so a mode it cannot take must not even produce a chat
		// (ensureRPC fails closed on it too).
		if _, _, _, ok := codexModeSettings(permissionMode); permissionMode != "" && !ok {
			return nil, fmt.Errorf("unsupported Codex permission mode %q", permissionMode)
		}
	}

	// Launch settings live on the row for every transport; the process starts
	// on the first chat_send (the chat runtime), not here.
	chat, err := a.CreateChat(Chat{
		WorkspaceID: workspaceID,
		// The " (phone)" is not decoration. The desktop names chats
		// `Chat <n>` too, so a remotely-created one used to be
		// indistinguishable from the fifty above it in the sidebar — a chat
		// that exists and cannot be found is not much better than one that
		// does not.
		Title:          fmt.Sprintf("Chat %d (phone)", countForWs+1),
		AgentKind:      agentKind,
		Transport:      transport,
		Model:          model,
		Effort:         effort,
		PermissionMode: permissionMode,
		LastActivityAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	return remoteChatShape(chat, names, paths), nil
}

// RemoteSetChatTitle upgrades a chat's title from the phone — first the cheap
// local heuristic off the prompt, then (fire-and-forget) the model-written one
// from generate_chat_title, mirroring AgentChat.vue's smartTitle→refineTitle
// two-step. expectTitle is a compare-and-swap: only replace the title this
// caller actually saw, so a slower of two concurrent refinements (or a real
// rename, once one exists) cannot stomp on a newer title with a stale one.
//
// Deliberately narrower than save_chats: that upserts a full desktop-shaped
// Chat row, and RemoteListChats's shape the phone actually holds never carries
// pinned_title/branch/model/settled_override/control — building a full row
// from what the phone has would silently blank every one of them.
func (a *App) RemoteSetChatTitle(id int64, title, expectTitle string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title must not be empty")
	}
	res, err := a.db.Exec(
		`UPDATE chats SET title = ? WHERE id = ? AND pinned_title = 0 AND title = ?`,
		title, id, expectTitle,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		busEmit("chats-changed", nil)
	}
	return nil
}
