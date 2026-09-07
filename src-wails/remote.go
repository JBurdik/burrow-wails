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
		// Mission Control's hidden session is not a user-facing chat — the
		// desktop sidebar hides it for the same reason.
		if c.Control {
			continue
		}
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

// RemoteCreateChat creates a chat and starts its CLI.
//
// The long warning that used to sit here is obsolete: the id comes from the
// database, so it cannot be reissued to somebody else, and no other client's
// save can revert this row (see chats.go). `remoteCreateMu` went with it —
// it only ever serialized two RemoteCreateChat calls against a hazard the
// database now owns, and its own comment admitted it never closed the real
// one.
//
// Claude-only for now: an ACP/Codex session needs command/args/configDir
// resolved from provider config that today only exists in AgentChat.vue's
// acpStartPayload().
func (a *App) RemoteCreateChat(workspaceID int64, agentKind string) (map[string]any, error) {
	if agentKind != "claude" {
		return nil, fmt.Errorf("remote chat creation only supports Claude for now (got %q)", agentKind)
	}

	// Resolve and validate the workspace BEFORE creating anything, so an
	// invalid request never leaves a row behind.
	names, paths := a.workspaceLabels()
	cwd, err := resolveWorkspaceCwd(paths, workspaceID)
	if err != nil {
		return nil, err
	}

	existing, err := a.ListChats()
	if err != nil {
		return nil, err
	}
	countForWs := 0
	for _, c := range existing {
		if c.WorkspaceID == workspaceID && !c.Control {
			countForWs++
		}
	}

	chat, err := a.CreateChat(Chat{
		WorkspaceID: workspaceID,
		// The " (phone)" is not decoration. The desktop names chats
		// `Chat <n>` too, so a remotely-created one used to be
		// indistinguishable from the fifty above it in the sidebar — a chat
		// that exists and cannot be found is not much better than one that
		// does not.
		Title:          fmt.Sprintf("Chat %d (phone)", countForWs+1),
		AgentKind:      agentKind,
		Transport:      "claude-cli",
		LastActivityAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}

	if err := a.ClaudeStart(fmt.Sprint(chat.ID), cwd, "", "default", "", "", "", "", "", ""); err != nil {
		// Roll the row back. A chat whose CLI never started is a ghost in
		// both sidebars that can only be removed by hand — and under
		// config.json that is exactly what a failed start used to leave.
		if delErr := a.DeleteChat(chat.ID); delErr != nil {
			return nil, fmt.Errorf("start claude: %w (and rolling back chat %d failed: %v)", err, chat.ID, delErr)
		}
		return nil, fmt.Errorf("start claude: %w", err)
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
