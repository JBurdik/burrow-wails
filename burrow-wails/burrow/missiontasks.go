package main

import "encoding/json"

func parseJSONStringArray(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// ListMissionTasks/UpsertMissionTask/DeleteMissionTask operate on the same
// MissionTask type as board.go — see its comment for why (one shared table
// + frontend type for both the kanban board and the plain task list).

func (a *App) ListMissionTasks() ([]MissionTask, error) {
	rows, err := a.db.Query("SELECT " + missionTaskCols + " FROM mission_tasks ORDER BY created_at DESC")
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

func (a *App) UpsertMissionTask(t MissionTask) error {
	return a.UpsertBoardTask(t)
}

func (a *App) DeleteMissionTask(id string) error {
	return a.DeleteBoardTask(id)
}

// --- agent turns (per-run diff tracking) ---
// AgentTurnChange mirrors the frontend's AgentTurnChange interface
// (src/stores/boardTasks.ts) — NOTE this one is camelCase, unlike
// MissionTask; that's what the original Rust struct actually serialized.
type AgentTurnChange struct {
	ID               int64    `json:"id"`
	TaskID           string   `json:"taskId"`
	PtyID            int64    `json:"ptyId"`
	StartedAt        int64    `json:"startedAt"`
	CompletedAt      *int64   `json:"completedAt,omitempty"`
	State            string   `json:"state"`
	ChangesAvailable bool     `json:"changesAvailable"`
	ChangeError      *string  `json:"changeError,omitempty"`
	Files            []string `json:"files"`
	Additions        int      `json:"additions"`
	Deletions        int      `json:"deletions"`
}

func (a *App) BeginAgentTurn(taskID string, ptyID int64, worktreePath string) (int64, error) {
	res, err := a.db.Exec(`INSERT INTO agent_turns (task_id, pty_id, worktree_path, started_at, state) VALUES (?, ?, ?, ?, 'running')`,
		taskID, ptyID, worktreePath, nowMillis())
	if err != nil {
		return 0, err
	}
	_, _ = a.db.Exec(`UPDATE mission_tasks SET turns = turns + 1 WHERE id = ?`, taskID)
	return res.LastInsertId()
}

func (a *App) CompleteAgentTurn(ptyID int64, state string) error {
	_, err := a.db.Exec(`UPDATE agent_turns SET state = ?, completed_at = ? WHERE pty_id = ? AND completed_at IS NULL`,
		state, nowMillis(), ptyID)
	if a.ctx != nil {
		emitAgentDone(a.ctx)
	}
	return err
}

func (a *App) ListAgentTurnChanges(taskID string) ([]AgentTurnChange, error) {
	rows, err := a.db.Query(`SELECT id, task_id, pty_id, started_at, completed_at, state, changes_available, change_error, files_json, additions, deletions
		FROM agent_turns WHERE task_id = ? ORDER BY started_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentTurnChange
	for rows.Next() {
		var t AgentTurnChange
		var changesAvailable *int
		var filesJSON string
		var additions, deletions *int
		if err := rows.Scan(&t.ID, &t.TaskID, &t.PtyID, &t.StartedAt, &t.CompletedAt, &t.State, &changesAvailable, &t.ChangeError, &filesJSON, &additions, &deletions); err != nil {
			return nil, err
		}
		t.ChangesAvailable = changesAvailable != nil && *changesAvailable != 0
		if additions != nil {
			t.Additions = *additions
		}
		if deletions != nil {
			t.Deletions = *deletions
		}
		t.Files = parseJSONStringArray(filesJSON)
		out = append(out, t)
	}
	return out, rows.Err()
}
