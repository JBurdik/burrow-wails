package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SpawnRequest mirrors the Rust struct of the same name (src-tauri/src/lib.rs)
// — one queued action dropped by the `burrow` CLI as a request dir under
// $BURROW_SESSION_DIR/requests/. Read-type commands (list-workspaces,
// list-tabs) are answered directly here; UI-action kinds (spawn, worktree,
// focus-workspace, focus-tab, new-tab, tab-rename, tab-close,
// workspace-create) are returned to the frontend poll loop to perform.
type SpawnRequest struct {
	Kind    string `json:"kind"`
	Cmd     string `json:"cmd"`
	Token   string `json:"token"`
	Cwd     string `json:"cwd"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	TmuxWin string `json:"tmuxWin"`
	Wsid    string `json:"wsid"`
	Tabid   string `json:"tabid"`
	Content string `json:"content"`
}

// TakeSpawnRequests polls $BURROW_SESSION_DIR/requests for ready request
// dirs, claims (deletes) each one owned by cwd, answers read-commands
// in-process, and returns the rest for the frontend to act on.
func (a *App) TakeSpawnRequests(cwd string) []SpawnRequest {
	out := []SpawnRequest{}
	if a.sessionDir == "" {
		return out
	}
	reqDir := filepath.Join(a.sessionDir, "requests")
	entries, err := os.ReadDir(reqDir)
	if err != nil {
		return out
	}

	for _, e := range entries {
		d := filepath.Join(reqDir, e.Name())
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(d, "ready")); err != nil {
			continue
		}

		read := func(name string) string {
			b, _ := os.ReadFile(filepath.Join(d, name))
			return strings.TrimSpace(string(b))
		}
		ws := read("ws")
		kind := read("kind")
		token := read("token")
		wsid := read("wsid")
		tabid := read("tabid")
		cmd := read("cmd")

		switch kind {
		case "list-workspaces":
			if ws != cwd {
				continue
			}
			os.RemoveAll(d)
			a.writeControlResult(token, a.tsvWorkspaces())
			continue
		case "list-tabs":
			if ws != cwd {
				continue
			}
			os.RemoveAll(d)
			a.writeControlResult(token, a.tsvTabs(wsid, ws))
			continue
		}

		if ws != cwd && kind != "spawn" && kind != "" && kind != "worktree" {
			// Everything except spawn/worktree/default routes to the origin
			// workspace only (see take_spawn_requests in src-tauri/src/lib.rs).
			continue
		}
		os.RemoveAll(d)

		req := SpawnRequest{
			Kind:    kind,
			Cmd:     cmd,
			Token:   token,
			Cwd:     read("cwd"),
			Branch:  read("branch"),
			Base:    read("base"),
			TmuxWin: read("tmux_win"),
			Wsid:    wsid,
			Tabid:   tabid,
			Content: read("content"),
		}
		switch kind {
		case "tab-rename", "tab-close", "workspace-create":
			req.Cmd = read("name")
			req.Cwd = read("path")
			req.Base = read("force")
			req.Token = ""
		}
		out = append(out, req)
	}
	return out
}

func (a *App) writeControlResult(token, text string) {
	if token == "" {
		return
	}
	base := filepath.Join(a.sessionDir, token)
	_ = os.WriteFile(base+".result", []byte(text), 0o644)
	_ = os.WriteFile(base+".done", nil, 0o644)
}

func (a *App) tsvWorkspaces() string {
	rows, err := a.db.Query(`SELECT id, name, path FROM workspaces ORDER BY sort_order, id`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, path string
		if rows.Scan(&id, &name, &path) != nil {
			continue
		}
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteByte('\t')
		b.WriteString(name)
		b.WriteByte('\t')
		b.WriteString(path)
		b.WriteByte('\n')
	}
	return b.String()
}

func (a *App) tsvTabs(wsid, originPath string) string {
	var workspaceID int64
	if wsid != "" {
		workspaceID, _ = strconv.ParseInt(wsid, 10, 64)
	} else {
		_ = a.db.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, originPath).Scan(&workspaceID)
	}
	if workspaceID == 0 {
		return ""
	}
	rows, err := a.db.Query(`SELECT pty_id, title FROM terminal_tabs WHERE workspace_id = ? ORDER BY ord`, workspaceID)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var ptyID, title *string
		if rows.Scan(&ptyID, &title) != nil {
			continue
		}
		if ptyID != nil {
			b.WriteString(*ptyID)
		}
		b.WriteByte('\t')
		if title != nil {
			b.WriteString(*title)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
