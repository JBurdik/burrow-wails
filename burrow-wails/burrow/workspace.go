package main

// Workspace mirrors the `workspaces` table / Rust struct in lib.rs.
type Workspace struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Path           string  `json:"path"`
	CreatedAt      string  `json:"createdAt"`
	LastOpened     *string `json:"lastOpened,omitempty"`
	ParentID       *int64  `json:"parentId,omitempty"`
	WorktreeBranch *string `json:"worktreeBranch,omitempty"`
	IsGit          bool    `json:"isGit"`
	Icon           *string `json:"icon,omitempty"`
	SortOrder      float64 `json:"sortOrder"`
}

const workspaceCols = "id, name, path, created_at, last_opened, parent_id, worktree_branch, is_git, icon, sort_order"

func scanWorkspace(row interface {
	Scan(dest ...any) error
}) (Workspace, error) {
	var w Workspace
	var isGit int
	err := row.Scan(&w.ID, &w.Name, &w.Path, &w.CreatedAt, &w.LastOpened, &w.ParentID, &w.WorktreeBranch, &isGit, &w.Icon, &w.SortOrder)
	w.IsGit = isGit != 0
	return w, err
}

func (a *App) ListWorkspaces() ([]Workspace, error) {
	rows, err := a.db.Query("SELECT " + workspaceCols + " FROM workspaces ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (a *App) CreateWorkspace(name, path string) (Workspace, error) {
	res, err := a.db.Exec(`INSERT INTO workspaces (name, path) VALUES (?, ?)`, name, path)
	if err != nil {
		return Workspace{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, err
	}
	row := a.db.QueryRow("SELECT "+workspaceCols+" FROM workspaces WHERE id = ?", id)
	return scanWorkspace(row)
}

func (a *App) DeleteWorkspace(id int64) error {
	_, err := a.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
	return err
}

func (a *App) RenameWorkspace(id int64, name string) error {
	_, err := a.db.Exec(`UPDATE workspaces SET name = ? WHERE id = ?`, name, id)
	return err
}

func (a *App) TouchWorkspace(id int64) error {
	_, err := a.db.Exec(`UPDATE workspaces SET last_opened = datetime('now') WHERE id = ?`, id)
	return err
}

func (a *App) SetWorkspaceIcon(id int64, icon string) error {
	_, err := a.db.Exec(`UPDATE workspaces SET icon = ? WHERE id = ?`, icon, id)
	return err
}

func (a *App) SetWorkspaceOrder(ids []int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE workspaces SET sort_order = ? WHERE id = ?`, float64(i), id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// --- terminal tabs ---

type TerminalTab struct {
	ID            int64   `json:"id"`
	WorkspaceID   int64   `json:"workspaceId"`
	Ord           int     `json:"ord"`
	Title         *string `json:"title,omitempty"`
	InitialCmd    *string `json:"initialCmd,omitempty"`
	PtyID         *string `json:"ptyId,omitempty"`
	Cwd           *string `json:"cwd,omitempty"`
	DefaultTitle  *string `json:"defaultTitle,omitempty"`
	SessionID     *string `json:"sessionId,omitempty"`
}

func (a *App) ListTerminalTabs(workspaceID int64) ([]TerminalTab, error) {
	rows, err := a.db.Query(`SELECT id, workspace_id, ord, title, initial_cmd, pty_id, cwd, default_title, session_id
		FROM terminal_tabs WHERE workspace_id = ? ORDER BY ord`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TerminalTab
	for rows.Next() {
		var t TerminalTab
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Ord, &t.Title, &t.InitialCmd, &t.PtyID, &t.Cwd, &t.DefaultTitle, &t.SessionID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) SaveTerminalTabs(workspaceID int64, tabs []TerminalTab) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM terminal_tabs WHERE workspace_id = ?`, workspaceID); err != nil {
		tx.Rollback()
		return err
	}
	for i, t := range tabs {
		if _, err := tx.Exec(`INSERT INTO terminal_tabs (workspace_id, ord, title, initial_cmd, pty_id, cwd, default_title, session_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, i, t.Title, t.InitialCmd, t.PtyID, t.Cwd, t.DefaultTitle, t.SessionID); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
