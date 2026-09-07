package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runGitOrFatal is a small helper for setting up a real repo under test —
// CreateWorktree shells out to the real `git` binary, so there is no
// reasonable fake for "a repo with a worktree".
func runGitOrFatal(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestCreateWorktreeEmitsAfterRowIsNested is Important 2 from the final
// whole-plan review: CreateWorktree calls CreateWorkspace (which emits
// workspaces-changed as soon as the row exists) and only THEN runs the
// UPDATE that sets parent_id/worktree_branch/is_git — so a client reloading
// on that first emit could see the worktree as a top-level, non-git
// workspace. This asserts the fix: whichever emit is LAST, by the time it
// fires the row in the database is already fully nested.
func TestCreateWorktreeEmitsAfterRowIsNested(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGitOrFatal(t, repoPath, "init")
	runGitOrFatal(t, repoPath, "config", "user.email", "test@example.com")
	runGitOrFatal(t, repoPath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitOrFatal(t, repoPath, "add", ".")
	runGitOrFatal(t, repoPath, "commit", "-m", "init")

	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	a := &App{db: db}

	parent, err := a.CreateWorkspace("repo", repoPath)
	if err != nil {
		t.Fatalf("create parent workspace: %v", err)
	}

	t.Cleanup(busReset)
	busReset()

	var (
		emits      int
		lastNested bool
	)
	unsub := busSubscribe(func(ev shellEvent) {
		if ev.Name != "workspaces-changed" {
			return
		}
		emits++
		var parentID *int64
		var branch *string
		var isGit bool
		row := db.QueryRow(`SELECT parent_id, worktree_branch, is_git FROM workspaces WHERE path = ?`, filepath.Join(dir, "wt"))
		if err := row.Scan(&parentID, &branch, &isGit); err != nil {
			// The row may not exist yet if this is the premature emit from
			// inside CreateWorkspace, which is the exact case this test
			// exists to distinguish from the final, correct emit.
			lastNested = false
			return
		}
		lastNested = parentID != nil && *parentID == parent.ID && branch != nil && *branch == "feature" && isGit
	})
	defer unsub()

	worktreePath := filepath.Join(dir, "wt")
	ws, err := a.CreateWorktree(repoPath, "wt", worktreePath, "feature", "")
	if err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	if ws.ParentID == nil || *ws.ParentID != parent.ID {
		t.Fatalf("returned workspace not nested: %+v", ws)
	}

	if emits == 0 {
		t.Fatalf("CreateWorktree emitted no workspaces-changed event")
	}
	if !lastNested {
		t.Fatalf("workspace row was not fully nested (parent_id/worktree_branch/is_git) by the time the last emit fired")
	}
}
