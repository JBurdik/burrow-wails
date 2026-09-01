package control

import (
	"context"
	"fmt"
)

// Navigation verbs: what the app currently shows, and moving the user's view
// around it. The read half answers from SQLite (the frontend persists tabs and
// their status there, so the DB is an accurate mirror without a UI round-trip).
// The write half can only be done by the frontend, so it goes through UIBridge
// and reports what the UI acked with.

// Workspace is the shape returned to clients. Deliberately narrower than the
// app's own Workspace row: a Manager needs identity, location and whether this
// is a worktree — not sort order or icons.
type Workspace struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	ParentID *int64 `json:"parent_id,omitempty"`
	Branch   string `json:"branch,omitempty"`
}

// Tab is one terminal tab (or chat) in a workspace.
type Tab struct {
	PtyID  int64  `json:"pty_id"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

func navigationVerbs(c *Core) []Verb {
	return []Verb{{
		Name:    "list_workspaces",
		Summary: "Every workspace Burrow knows, including worktrees (parent_id set)",
		Scope:   ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.listWorkspaces(ctx)
		},
	}, {
		Name:    "list_tabs",
		Summary: "Tabs in a workspace, with each agent's current status",
		Args: []Arg{
			{Name: "workspace_id", Type: "integer", Desc: "Workspace to list; defaults to the caller's own"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.listTabs(ctx, p.Int("workspace_id"), p.Str("cwd"))
		},
	}, {
		Name:    "focus_workspace",
		Summary: "Switch the user's view to a workspace (opening it if needed)",
		Args:    []Arg{{Name: "workspace_id", Type: "integer", Desc: "Workspace to focus", Required: true}},
		Scope:   ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "focus_workspace", map[string]any{"workspaceId": p.Int("workspace_id")})
		},
	}, {
		Name:    "focus_tab",
		Summary: "Activate a tab by its pty id, switching workspace first if needed",
		Args:    []Arg{{Name: "pty_id", Type: "integer", Desc: "Tab to focus", Required: true}},
		Scope:   ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "focus_tab", map[string]any{"ptyId": p.Int("pty_id")})
		},
	}, {
		Name:    "new_tab",
		Summary: "Open an empty terminal tab (use spawn for an agent)",
		Args: []Arg{
			{Name: "workspace_id", Type: "integer", Desc: "Where to open it; defaults to the active workspace"},
			{Name: "cmd", Type: "string", Desc: "Command to run in the new tab"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "new_tab", map[string]any{
				"workspaceId": p.Int("workspace_id"),
				"cmd":         p.Str("cmd"),
			})
		},
	}, {
		Name:    "tab_rename",
		Summary: "Retitle a tab",
		Args: []Arg{
			{Name: "pty_id", Type: "integer", Desc: "Tab to rename", Required: true},
			{Name: "title", Type: "string", Desc: "New title", Required: true},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "tab_rename", map[string]any{
				"ptyId": p.Int("pty_id"), "title": p.Str("title"),
			})
		},
	}, {
		Name:    "tab_close",
		Summary: "Close a tab and kill its PTY. Destructive — confirm with the user first",
		Args:    []Arg{{Name: "pty_id", Type: "integer", Desc: "Tab to close", Required: true}},
		Scope:   ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "tab_close", map[string]any{"ptyId": p.Int("pty_id")})
		},
	}, {
		Name:    "diagram",
		Summary: "Show a Mermaid diagram to the user in a panel",
		Args:    []Arg{{Name: "content", Type: "string", Desc: "Mermaid source", Required: true}},
		Scope:   ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "diagram", map[string]any{"content": p.Str("content")})
		},
	}, {
		Name:    "workspace_create",
		Summary: "Add a directory as a new workspace and open it",
		Args: []Arg{
			{Name: "path", Type: "string", Desc: "Absolute path to the project", Required: true},
			{Name: "name", Type: "string", Desc: "Display name; defaults to the directory name"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.uiAction(ctx, "workspace_create", map[string]any{
				"path": p.Str("path"), "name": p.Str("name"),
			})
		},
	}}
}

// uiAction is the shared tail of every UI-performed verb: hand it over, return
// whatever the frontend acked with (usually ids), or the reason it couldn't.
func (c *Core) uiAction(ctx context.Context, action string, args map[string]any) (any, error) {
	var out map[string]any
	if err := c.ui(ctx, action, args, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	return out, nil
}

func (c *Core) listWorkspaces(_ context.Context) ([]Workspace, error) {
	rows, err := c.deps.DB.Query(`SELECT id, name, path, parent_id, worktree_branch FROM workspaces ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		var branch *string
		if err := rows.Scan(&w.ID, &w.Name, &w.Path, &w.ParentID, &branch); err != nil {
			return nil, err
		}
		if branch != nil {
			w.Branch = *branch
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// listTabs resolves the workspace by id, else by the caller's cwd — an agent
// knows the directory it runs in, not the workspace row id.
func (c *Core) listTabs(_ context.Context, workspaceID int64, cwd string) ([]Tab, error) {
	if workspaceID == 0 {
		if cwd == "" {
			return nil, fmt.Errorf("list_tabs: pass workspace_id (or call from a workspace dir)")
		}
		if err := c.deps.DB.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, cwd).Scan(&workspaceID); err != nil {
			return nil, fmt.Errorf("list_tabs: %s is not a workspace", cwd)
		}
	}
	rows, err := c.deps.DB.Query(`SELECT pty_id, title, status FROM terminal_tabs WHERE workspace_id = ? ORDER BY ord`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Tab{}
	for rows.Next() {
		var ptyID *int64
		var title, status *string
		if err := rows.Scan(&ptyID, &title, &status); err != nil {
			return nil, err
		}
		t := Tab{}
		if ptyID != nil {
			t.PtyID = *ptyID
		}
		if title != nil {
			t.Title = *title
		}
		if status != nil {
			t.Status = *status
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
