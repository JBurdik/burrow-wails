package main

import (
	"database/sql"
	"strings"
)

// WorkingTreeDiff compares HEAD with one snapshot, including untracked files.
// A single comparison avoids duplicate patches for staged + unstaged edits of
// the same file and also works before the repository's first commit.
func (a *App) WorkingTreeDiff(cwd string) (string, error) {
	if !isGitRepo(cwd) {
		return "", nil
	}
	tree, err := snapshotTree(cwd)
	if err != nil {
		return "", err
	}
	base := "HEAD"
	if !runGitEnv(cwd, nil, "rev-parse", "--verify", "HEAD").Success {
		empty := runGitEnv(cwd, nil, "mktree")
		if !empty.Success {
			return "", gitErr(empty)
		}
		base = strings.TrimSpace(empty.Stdout)
	}
	out := runGitEnv(cwd, nil, "diff", "--no-ext-diff", "--no-color", base, tree)
	if !out.Success {
		return "", gitErr(out)
	}
	return out.Stdout, nil
}

// LastTurnAudit returns the last settled turn of the selected thread, never
// another thread's work or a still-running turn. Its diff is already frozen.
func (a *App) LastTurnAudit(cwd, subjectID string) (*TurnAudit, error) {
	if a.db == nil || cwd == "" || subjectID == "" {
		return nil, nil
	}
	var audit TurnAudit
	var files string
	err := a.db.QueryRow(`SELECT id, subject_id, cwd, label, checkpoint_sha,
		started_at, settled_at, state, diff, files FROM turn_audits
		WHERE cwd = ? AND subject_id = ? AND settled_at > 0
		ORDER BY id DESC LIMIT 1`, cwd, subjectID).Scan(&audit.ID, &audit.SubjectID,
		&audit.Cwd, &audit.Label, &audit.Checkpoint, &audit.StartedAt, &audit.SettledAt,
		&audit.State, &audit.Diff, &files)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	audit.Files = []string{}
	if files != "" {
		audit.Files = strings.Split(files, "\n")
	}
	return &audit, nil
}
