package main

// MissionTask mirrors the frontend's MissionTask interface exactly
// (src/stores/boardTasks.ts) — the `mission_tasks` table backs BOTH the
// kanban board (board_column/board_order) and the plain Mission Control
// task list; the frontend uses one shared type for both, so this struct
// does too (board.go and missiontasks.go both operate on it).
type MissionTask struct {
	ID              string  `json:"id"`
	WorkspaceID     *int64  `json:"workspace_id"`
	PtyID           *int64  `json:"pty_id,omitempty"`
	Title           string  `json:"title"`
	Cwd             *string `json:"cwd,omitempty"`
	Model           *string `json:"model,omitempty"`
	Status          *string `json:"status,omitempty"`
	Turns           *int    `json:"turns,omitempty"`
	CreatedAt       int64   `json:"created_at"`
	HandedOff       *int    `json:"handed_off,omitempty"`
	ProfileID       *string `json:"profile_id,omitempty"`
	RepoWorkspaceID *int64  `json:"repo_workspace_id"`
	BoardColumn     string  `json:"board_column"`
	Description     *string `json:"description,omitempty"`
	AgentKind       *string `json:"agent_kind,omitempty"`
	Transport       *string `json:"transport,omitempty"`
	UseWorktree     *int    `json:"use_worktree,omitempty"`
	WorktreeBranch  *string `json:"worktree_branch,omitempty"`
	TaskWorkspaceID *int64  `json:"task_workspace_id,omitempty"`
	ChatID          *int64  `json:"chat_id,omitempty"`
	SessionID       *string `json:"session_id,omitempty"`
	BoardOrder      float64 `json:"board_order"`
	UpdatedAt       *int64  `json:"updated_at,omitempty"`
}

const missionTaskCols = `id, workspace_id, pty_id, title, cwd, model, status, turns, created_at,
	handed_off, profile_id, repo_workspace_id, board_column, description, agent_kind, transport,
	use_worktree, worktree_branch, task_workspace_id, chat_id, session_id, board_order, updated_at`

func scanMissionTask(row interface{ Scan(dest ...any) error }) (MissionTask, error) {
	var t MissionTask
	err := row.Scan(&t.ID, &t.WorkspaceID, &t.PtyID, &t.Title, &t.Cwd, &t.Model, &t.Status, &t.Turns, &t.CreatedAt,
		&t.HandedOff, &t.ProfileID, &t.RepoWorkspaceID, &t.BoardColumn, &t.Description, &t.AgentKind, &t.Transport,
		&t.UseWorktree, &t.WorktreeBranch, &t.TaskWorkspaceID, &t.ChatID, &t.SessionID, &t.BoardOrder, &t.UpdatedAt)
	return t, err
}

func (a *App) ListBoardTasks(repoWorkspaceID int64) ([]MissionTask, error) {
	rows, err := a.db.Query("SELECT "+missionTaskCols+" FROM mission_tasks WHERE repo_workspace_id = ? ORDER BY board_column, board_order", repoWorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MissionTask
	for rows.Next() {
		t, err := scanMissionTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) UpsertBoardTask(t MissionTask) error {
	if t.CreatedAt == 0 {
		t.CreatedAt = nowMillis()
	}
	now := nowMillis()
	_, err := a.db.Exec(`INSERT INTO mission_tasks
		(id, workspace_id, pty_id, title, cwd, model, status, turns, created_at,
		 handed_off, profile_id, repo_workspace_id, board_column, description, agent_kind, transport,
		 use_worktree, worktree_branch, task_workspace_id, chat_id, session_id, board_order, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id = excluded.workspace_id, pty_id = excluded.pty_id, title = excluded.title,
			cwd = excluded.cwd, model = excluded.model, status = excluded.status, turns = excluded.turns,
			handed_off = excluded.handed_off, profile_id = excluded.profile_id,
			repo_workspace_id = excluded.repo_workspace_id, board_column = excluded.board_column,
			description = excluded.description, agent_kind = excluded.agent_kind, transport = excluded.transport,
			use_worktree = excluded.use_worktree, worktree_branch = excluded.worktree_branch,
			task_workspace_id = excluded.task_workspace_id, chat_id = excluded.chat_id,
			session_id = excluded.session_id, board_order = excluded.board_order, updated_at = excluded.updated_at`,
		t.ID, t.WorkspaceID, t.PtyID, t.Title, t.Cwd, t.Model, t.Status, t.Turns, t.CreatedAt,
		t.HandedOff, t.ProfileID, t.RepoWorkspaceID, t.BoardColumn, t.Description, t.AgentKind, t.Transport,
		t.UseWorktree, t.WorktreeBranch, t.TaskWorkspaceID, t.ChatID, t.SessionID, t.BoardOrder, now)
	if err != nil {
		return err
	}
	if a.ctx != nil {
		emitBoardTaskMoved(a.ctx)
	}
	return nil
}

func (a *App) MoveBoardTask(taskID, column string, order float64) error {
	_, err := a.db.Exec(`UPDATE mission_tasks SET board_column = ?, board_order = ?, updated_at = ? WHERE id = ?`, column, order, nowMillis(), taskID)
	if err != nil {
		return err
	}
	if a.ctx != nil {
		emitBoardTaskMoved(a.ctx)
	}
	return nil
}

func (a *App) DeleteBoardTask(taskID string) error {
	_, err := a.db.Exec(`DELETE FROM mission_tasks WHERE id = ?`, taskID)
	return err
}
