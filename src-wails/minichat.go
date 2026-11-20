package main

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Mini chat: a quick chat with no project, in its own small window (tray menu,
// tray panel, Window → Mini Chat). It lives in a "Scratch" workspace backed by
// <app-data>/scratch, so the chat runtime, the Sidebar and the main window
// treat it like any other chat — there is no workspace_id = 0 case anywhere.
const (
	miniChatTitle  = "Mini chat"
	miniChatWinKey = "minichat"
)

func (a *App) scratchWorkspace() (Workspace, error) {
	base, err := appDataDir()
	if err != nil {
		return Workspace{}, err
	}
	dir := filepath.Join(base, "scratch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Workspace{}, err
	}
	w, err := scanWorkspace(a.db.QueryRow("SELECT "+workspaceCols+" FROM workspaces WHERE path = ?", dir))
	if errors.Is(err, sql.ErrNoRows) {
		return a.CreateWorkspace("Scratch", dir)
	}
	return w, err
}

// MiniChat returns the chat the mini window shows: the most recent unarchived
// top-level chat in the scratch workspace, or a new one (none yet, or fresh).
func (a *App) MiniChat(fresh bool) (map[string]any, error) {
	w, err := a.scratchWorkspace()
	if err != nil {
		return nil, err
	}
	var id int64
	if !fresh {
		_ = a.db.QueryRow(`SELECT id FROM chats WHERE workspace_id = ? AND archived_at = 0 AND parent_chat_id = 0
			ORDER BY last_activity_at DESC, id DESC LIMIT 1`, w.ID).Scan(&id)
	}
	if id == 0 {
		c, err := a.CreateChat(Chat{
			WorkspaceID: w.ID, Title: miniChatTitle, AgentKind: "claude", Transport: "claude-cli",
			LastActivityAt: time.Now().UnixMilli(),
		})
		if err != nil {
			return nil, err
		}
		id = c.ID
	}
	return map[string]any{"chatId": id, "workspaceId": w.ID, "cwd": w.Path, "route": miniChatRoute(id, w.ID, w.Path)}, nil
}

// miniChatRoute is WindowApp's "chat" kind; mini=1 adds the pin / new buttons.
func miniChatRoute(chatID, wsID int64, cwd string) string {
	q := url.Values{}
	q.Set("chat", strconv.FormatInt(chatID, 10))
	q.Set("ws", strconv.FormatInt(wsID, 10))
	q.Set("cwd", cwd)
	q.Set("title", miniChatTitle)
	q.Set("mini", "1")
	return "/window/chat/0?" + q.Encode()
}
