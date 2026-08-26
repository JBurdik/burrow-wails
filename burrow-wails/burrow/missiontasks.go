package main

import "time"

// MissionTask mirrors a row of `mission_tasks` as used by the Mission
// Control view (as opposed to the kanban board — see board.go), matching
// upsert_mission_task/delete_mission_task in src-tauri/src/lib.rs.
type MissionTask struct {
	ID          string  `json:"id"`
	WorkspaceID *int64  `json:"workspaceId,omitempty"`
	PtyID       *string `json:"ptyId,omitempty"`
	Title       string  `json:"title"`
	Cwd         *string `json:"cwd,omitempty"`
	Model       *string `json:"model,omitempty"`
	Status      *string `json:"status,omitempty"`
	Turns       int     `json:"turns"`
}

func (a *App) ListMissionTasks() ([]MissionTask, error) {
	rows, err := a.db.Query(`SELECT id, workspace_id, pty_id, title, cwd, model, status, turns FROM mission_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MissionTask
	for rows.Next() {
		var t MissionTask
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.PtyID, &t.Title, &t.Cwd, &t.Model, &t.Status, &t.Turns); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) UpsertMissionTask(t MissionTask) error {
	_, err := a.db.Exec(`INSERT INTO mission_tasks (id, workspace_id, pty_id, title, cwd, model, status, turns)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id = excluded.workspace_id,
			pty_id = excluded.pty_id,
			title = excluded.title,
			cwd = excluded.cwd,
			model = excluded.model,
			status = excluded.status,
			turns = excluded.turns`,
		t.ID, t.WorkspaceID, t.PtyID, t.Title, t.Cwd, t.Model, t.Status, t.Turns)
	return err
}

func (a *App) DeleteMissionTask(id string) error {
	_, err := a.db.Exec(`DELETE FROM mission_tasks WHERE id = ?`, id)
	return err
}

// --- agent turns (per-run diff tracking) ---

type AgentTurn struct {
	ID           int64   `json:"id"`
	TaskID       string  `json:"taskId"`
	PtyID        *string `json:"ptyId,omitempty"`
	WorktreePath *string `json:"worktreePath,omitempty"`
	StartedAt    string  `json:"startedAt"`
	CompletedAt  *string `json:"completedAt,omitempty"`
	State        string  `json:"state"`
}

func (a *App) BeginAgentTurn(taskID, ptyID, worktreePath string) (int64, error) {
	res, err := a.db.Exec(`INSERT INTO agent_turns (task_id, pty_id, worktree_path, state) VALUES (?, ?, ?, 'running')`, taskID, ptyID, worktreePath)
	if err != nil {
		return 0, err
	}
	_, _ = a.db.Exec(`UPDATE mission_tasks SET turns = turns + 1 WHERE id = ?`, taskID)
	return res.LastInsertId()
}

func (a *App) CompleteAgentTurn(ptyID, state string) error {
	_, err := a.db.Exec(`UPDATE agent_turns SET state = ?, completed_at = ? WHERE pty_id = ? AND completed_at IS NULL`,
		state, time.Now().UTC().Format(time.RFC3339), ptyID)
	if a.ctx != nil {
		emitAgentDone(a.ctx)
	}
	return err
}

func (a *App) ListAgentTurnChanges(taskID string) ([]AgentTurn, error) {
	rows, err := a.db.Query(`SELECT id, task_id, pty_id, worktree_path, started_at, completed_at, state FROM agent_turns WHERE task_id = ? ORDER BY started_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentTurn
	for rows.Next() {
		var t AgentTurn
		if err := rows.Scan(&t.ID, &t.TaskID, &t.PtyID, &t.WorktreePath, &t.StartedAt, &t.CompletedAt, &t.State); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
