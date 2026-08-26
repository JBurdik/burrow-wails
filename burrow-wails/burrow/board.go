package main

// BoardTask mirrors a row of `mission_tasks` as used by the kanban board
// (board_column/board_order), matching upsert_board_task/move_board_task/
// list_board_tasks/delete_board_task in src-tauri/src/lib.rs.
type BoardTask struct {
	ID              string  `json:"id"`
	RepoWorkspaceID *int64  `json:"repoWorkspaceId,omitempty"`
	Title           string  `json:"title"`
	Description     *string `json:"description,omitempty"`
	BoardColumn     string  `json:"boardColumn"`
	BoardOrder      float64 `json:"boardOrder"`
	Status          *string `json:"status,omitempty"`
	AgentKind       *string `json:"agentKind,omitempty"`
	UpdatedAt       *string `json:"updatedAt,omitempty"`
}

const boardTaskCols = "id, repo_workspace_id, title, description, board_column, board_order, status, agent_kind, updated_at"

func (a *App) ListBoardTasks(repoWorkspaceID int64) ([]BoardTask, error) {
	rows, err := a.db.Query("SELECT "+boardTaskCols+" FROM mission_tasks WHERE repo_workspace_id = ? ORDER BY board_column, board_order", repoWorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BoardTask
	for rows.Next() {
		var t BoardTask
		if err := rows.Scan(&t.ID, &t.RepoWorkspaceID, &t.Title, &t.Description, &t.BoardColumn, &t.BoardOrder, &t.Status, &t.AgentKind, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) UpsertBoardTask(t BoardTask) error {
	_, err := a.db.Exec(`INSERT INTO mission_tasks (id, repo_workspace_id, title, description, board_column, board_order, status, agent_kind, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			repo_workspace_id = excluded.repo_workspace_id,
			title = excluded.title,
			description = excluded.description,
			board_column = excluded.board_column,
			board_order = excluded.board_order,
			status = excluded.status,
			agent_kind = excluded.agent_kind,
			updated_at = datetime('now')`,
		t.ID, t.RepoWorkspaceID, t.Title, t.Description, t.BoardColumn, t.BoardOrder, t.Status, t.AgentKind)
	if err != nil {
		return err
	}
	if a.ctx != nil {
		emitBoardTaskMoved(a.ctx)
	}
	return nil
}

func (a *App) MoveBoardTask(taskID, column string, order float64) error {
	_, err := a.db.Exec(`UPDATE mission_tasks SET board_column = ?, board_order = ?, updated_at = datetime('now') WHERE id = ?`, column, order, taskID)
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
