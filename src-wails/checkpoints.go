package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Checkpoints snapshot a workspace's whole working tree (tracked + untracked,
// .gitignore respected) into a dangling git commit before every agent turn, so
// a turn that goes wrong can be reverted without the agent having committed
// anything. Snapshots never touch the real index or HEAD — they are built in a
// throwaway GIT_INDEX_FILE — and each one is anchored by a ref under
// refs/burrow/cp/ so `git gc` cannot prune it out from under us.

// Checkpoint mirrors the frontend's Checkpoint interface (src/stores/checkpoints.ts).
type Checkpoint struct {
	ID        int64  `json:"id"`
	Cwd       string `json:"cwd"`
	PtyID     string `json:"ptyId"`
	Label     string `json:"label"`
	Commit    string `json:"commit"`
	Tree      string `json:"tree"`
	CreatedAt int64  `json:"createdAt"`
}

// keepPerCwd bounds the ref/row growth per workspace. A long agent session
// makes one checkpoint per turn, so this is a few days of history.
// ponytail: fixed cap, not a setting — nobody scrolls back 200 turns.
const keepPerCwd = 100

const cpRefPrefix = "refs/burrow/cp/"

func runGitEnv(cwd string, env []string, args ...string) GitOutput {
	c := exec.Command("git", args...)
	if cwd != "" {
		c.Dir = cwd
	}
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}
	var stderr []byte
	stdout, err := c.Output()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return GitOutput{Stdout: string(stdout), Stderr: string(stderr), Code: code, Success: code == 0}
}

func gitLine(cwd string, args ...string) (string, bool) {
	out := runGitEnv(cwd, nil, args...)
	return strings.TrimSpace(out.Stdout), out.Success
}

// snapshotTree stages the entire working tree into a temporary index and writes
// it out as a tree object. The real index is untouched.
func snapshotTree(cwd string) (string, error) {
	idx := filepath.Join(os.TempDir(), fmt.Sprintf("burrow-index-%d", time.Now().UnixNano()))
	defer os.Remove(idx)
	env := []string{"GIT_INDEX_FILE=" + idx}

	// Seed from HEAD so `add -A` only has to walk the delta. Fails (harmlessly)
	// in a repo with no commits yet, where the empty index is already correct.
	runGitEnv(cwd, env, "read-tree", "HEAD")

	// ponytail: plain `add -A`, so anything the repo does not .gitignore lands
	// in the snapshot. If a workspace tracks huge un-ignored build output this
	// gets slow — fix the .gitignore before adding pathspec filtering here.
	if out := runGitEnv(cwd, env, "add", "-A"); !out.Success {
		return "", gitErr(out)
	}
	out := runGitEnv(cwd, env, "write-tree")
	if !out.Success {
		return "", gitErr(out)
	}
	return strings.TrimSpace(out.Stdout), nil
}

// snapshotCommit wraps snapshotTree in a commit object parented on HEAD, and
// pins it with a ref so gc keeps it. Returns (commit, tree).
func snapshotCommit(cwd, msg string) (string, string, error) {
	tree, err := snapshotTree(cwd)
	if err != nil {
		return "", "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	if head, ok := gitLine(cwd, "rev-parse", "HEAD"); ok && head != "" {
		args = append(args, "-p", head)
	}
	out := runGitEnv(cwd, nil, args...)
	if !out.Success {
		return "", "", gitErr(out)
	}
	commit := strings.TrimSpace(out.Stdout)
	runGitEnv(cwd, nil, "update-ref", cpRefPrefix+commit, commit)
	return commit, tree, nil
}

func isGitRepo(cwd string) bool {
	if cwd == "" {
		return false
	}
	top, ok := gitLine(cwd, "rev-parse", "--show-toplevel")
	return ok && top != ""
}

// CreateCheckpoint snapshots cwd. Returns a zero-id Checkpoint (no error) when
// there is nothing to record: not a git repo, or the tree is byte-identical to
// the newest checkpoint — an agent turn that changed no files gets no entry.
func (a *App) CreateCheckpoint(cwd, ptyID, label string) (Checkpoint, error) {
	if !isGitRepo(cwd) {
		return Checkpoint{}, nil
	}
	var lastTree string
	a.db.QueryRow(`SELECT tree_sha FROM checkpoints WHERE cwd = ? ORDER BY created_at DESC, id DESC LIMIT 1`, cwd).Scan(&lastTree)

	tree, err := snapshotTree(cwd)
	if err != nil {
		return Checkpoint{}, err
	}
	if tree == lastTree {
		return Checkpoint{}, nil
	}

	commit, tree, err := snapshotCommit(cwd, "burrow checkpoint: "+label)
	if err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{Cwd: cwd, PtyID: ptyID, Label: label, Commit: commit, Tree: tree, CreatedAt: nowMillis()}
	res, err := a.db.Exec(`INSERT INTO checkpoints (cwd, pty_id, label, commit_sha, tree_sha, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		cp.Cwd, cp.PtyID, cp.Label, cp.Commit, cp.Tree, cp.CreatedAt)
	if err != nil {
		return Checkpoint{}, err
	}
	cp.ID, _ = res.LastInsertId()
	a.pruneCheckpoints(cwd)
	return cp, nil
}

// pruneCheckpoints drops rows past keepPerCwd and unpins their refs, letting
// git gc reclaim the objects.
func (a *App) pruneCheckpoints(cwd string) {
	rows, err := a.db.Query(`SELECT id, commit_sha FROM checkpoints WHERE cwd = ? ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?`, cwd, keepPerCwd)
	if err != nil {
		return
	}
	type doomed struct {
		id     int64
		commit string
	}
	var list []doomed
	for rows.Next() {
		var d doomed
		if rows.Scan(&d.id, &d.commit) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		runGitEnv(cwd, nil, "update-ref", "-d", cpRefPrefix+d.commit)
		a.db.Exec(`DELETE FROM checkpoints WHERE id = ?`, d.id)
	}
}

func (a *App) ListCheckpoints(cwd string, limit int) ([]Checkpoint, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := a.db.Query(`SELECT id, cwd, pty_id, label, commit_sha, tree_sha, created_at
		FROM checkpoints WHERE cwd = ? ORDER BY created_at DESC, id DESC LIMIT ?`, cwd, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkpoint{}
	for rows.Next() {
		var cp Checkpoint
		var ptyID, label *string
		if err := rows.Scan(&cp.ID, &cp.Cwd, &ptyID, &label, &cp.Commit, &cp.Tree, &cp.CreatedAt); err != nil {
			return nil, err
		}
		if ptyID != nil {
			cp.PtyID = *ptyID
		}
		if label != nil {
			cp.Label = *label
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

// CheckpointDiff renders everything that changed between a checkpoint and the
// working tree right now. It diffs against a throwaway snapshot of the current
// state rather than the worktree directly, so files created since the
// checkpoint (still untracked) show up too.
func (a *App) CheckpointDiff(cwd, commit string) (string, error) {
	if !isGitRepo(cwd) {
		return "", errors.New("not a git repository")
	}
	nowTree, err := snapshotTree(cwd)
	if err != nil {
		return "", err
	}
	out := runGitEnv(cwd, nil, "diff", commit, nowTree)
	if !out.Success {
		return "", gitErr(out)
	}
	return out.Stdout, nil
}

// RestoreCheckpoint puts the working tree back to a checkpoint. It snapshots
// the current state first and returns that safety checkpoint, so a restore is
// itself undoable. The index is left where it was (pointing at HEAD) — this
// restores file contents, it does not stage or commit anything.
func (a *App) RestoreCheckpoint(cwd, commit string) (Checkpoint, error) {
	if !isGitRepo(cwd) {
		return Checkpoint{}, errors.New("not a git repository")
	}
	if ok := runGitEnv(cwd, nil, "cat-file", "-e", commit+"^{commit}").Success; !ok {
		return Checkpoint{}, errors.New("checkpoint object is gone (pruned by git gc?)")
	}

	safety, err := a.CreateCheckpoint(cwd, "", "before restore")
	if err != nil {
		return Checkpoint{}, fmt.Errorf("safety snapshot failed, refusing to restore: %w", err)
	}
	// Nothing changed since the last checkpoint, so the current state is
	// already pinned by that one — still safe to proceed.
	ref := safety.Commit
	if ref == "" {
		if last, err := a.ListCheckpoints(cwd, 1); err == nil && len(last) > 0 {
			ref = last[0].Commit
		}
	}

	// Files that exist now but did not at the checkpoint would survive a plain
	// `checkout <commit> -- .`, leaving a hybrid tree. Delete them explicitly.
	if ref != "" {
		out := runGitEnv(cwd, nil, "diff", "--name-only", "--diff-filter=A", commit, ref)
		for _, rel := range strings.Split(out.Stdout, "\n") {
			if rel = strings.TrimSpace(rel); rel != "" {
				os.Remove(filepath.Join(cwd, rel))
			}
		}
	}
	if out := runGitEnv(cwd, nil, "checkout", commit, "--", "."); !out.Success {
		return Checkpoint{}, gitErr(out)
	}
	// `checkout <commit> -- .` also stages what it wrote; put the index back to
	// HEAD so the user's staged/unstaged split is what it looks like on disk.
	runGitEnv(cwd, nil, "reset", "--quiet")
	return safety, nil
}
