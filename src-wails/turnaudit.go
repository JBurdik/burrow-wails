package main

import (
	"database/sql"
	"strings"
	"time"
)

// TurnAudit is the durable record of one agent turn. Checkpoints remain the
// recovery primitive; this record adds the lifecycle receipt and freezes the
// final diff so the audit never changes when later turns edit the workspace.
type TurnAudit struct {
	ID         int64    `json:"id"`
	SubjectID  string   `json:"subjectId"`
	Cwd        string   `json:"cwd"`
	Label      string   `json:"label"`
	Checkpoint string   `json:"checkpoint"`
	StartedAt  int64    `json:"startedAt"`
	SettledAt  int64    `json:"settledAt"`
	State      string   `json:"state"`
	Diff       string   `json:"diff"`
	Files      []string `json:"files"`
}

func turnAuditSchema() []string {
	return []string{`CREATE TABLE IF NOT EXISTS turn_audits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject_id TEXT NOT NULL,
		cwd TEXT NOT NULL,
		label TEXT NOT NULL DEFAULT '',
		checkpoint_sha TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		settled_at INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT 'running',
		diff TEXT NOT NULL DEFAULT '',
		files TEXT NOT NULL DEFAULT ''
	)`, `CREATE INDEX IF NOT EXISTS idx_turn_audits_subject ON turn_audits(subject_id, started_at DESC)`}
}

// StartTurnAudit takes the pre-turn checkpoint and records a running turn.
// Calls are idempotent while that subject already has a running audit: hooks
// and provider events can both observe the same start edge.
func (a *App) StartTurnAudit(cwd, subjectID, label string) (TurnAudit, error) {
	if a.db == nil || subjectID == "" {
		return TurnAudit{}, nil
	}
	var id int64
	if err := a.db.QueryRow(`SELECT id FROM turn_audits WHERE subject_id = ? AND state = 'running' ORDER BY id DESC LIMIT 1`, subjectID).Scan(&id); err == nil {
		return TurnAudit{ID: id}, nil
	}
	cp, err := a.CreateCheckpoint(cwd, subjectID, label)
	if err != nil {
		return TurnAudit{}, err
	}
	audit := TurnAudit{SubjectID: subjectID, Cwd: cwd, Label: label, Checkpoint: cp.Commit, StartedAt: time.Now().UnixMilli(), State: "running"}
	result, err := a.db.Exec(`INSERT INTO turn_audits (subject_id, cwd, label, checkpoint_sha, started_at, state) VALUES (?, ?, ?, ?, ?, 'running')`, audit.SubjectID, audit.Cwd, audit.Label, audit.Checkpoint, audit.StartedAt)
	if err != nil {
		return TurnAudit{}, err
	}
	audit.ID, _ = result.LastInsertId()
	return audit, nil
}

// SettleTurnAudit is the receipt boundary: after it returns, the final diff is
// frozen in SQLite and tests/UI may treat the turn as fully settled.
func (a *App) SettleTurnAudit(subjectID, state string) (TurnAudit, error) {
	if a.db == nil || subjectID == "" {
		return TurnAudit{}, nil
	}
	var audit TurnAudit
	var files string
	err := a.db.QueryRow(`SELECT id, subject_id, cwd, label, checkpoint_sha, started_at, settled_at, state, diff, files FROM turn_audits WHERE subject_id = ? AND state = 'running' ORDER BY id DESC LIMIT 1`, subjectID).Scan(&audit.ID, &audit.SubjectID, &audit.Cwd, &audit.Label, &audit.Checkpoint, &audit.StartedAt, &audit.SettledAt, &audit.State, &audit.Diff, &files)
	if err == sql.ErrNoRows {
		return TurnAudit{}, nil
	}
	if err != nil {
		return TurnAudit{}, err
	}
	if audit.Checkpoint != "" {
		if diff, diffErr := a.CheckpointDiff(audit.Cwd, audit.Checkpoint); diffErr == nil {
			audit.Diff = diff
		}
	}
	audit.Files = diffFiles(audit.Diff)
	files = strings.Join(audit.Files, "\n")
	audit.State, audit.SettledAt = state, time.Now().UnixMilli()
	_, err = a.db.Exec(`UPDATE turn_audits SET state = ?, settled_at = ?, diff = ?, files = ? WHERE id = ?`, audit.State, audit.SettledAt, audit.Diff, files, audit.ID)
	if err == nil {
		busEmit("turn-receipt-"+subjectID, audit)
	}
	return audit, err
}

func diffFiles(diff string) []string {
	seen, out := map[string]bool{}, []string{}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "diff --git a/") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		path := strings.TrimPrefix(parts[3], "b/")
		if !seen[path] {
			seen[path], out = true, append(out, path)
		}
	}
	return out
}
