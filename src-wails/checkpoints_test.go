package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A checkpoint has to survive the two things that actually happen during an
// agent turn: files edited, and files created that git has never seen. Restore
// has to put both back — including deleting whatever appeared afterwards.
func TestCheckpointRoundTrip(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out := runGitEnv(repo, nil, args...); !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	write("tracked.txt", "v1\n")
	git("add", ".")
	git("commit", "-qm", "init")

	app := &App{db: mustTestDB(t)}

	// Turn 1 starts: snapshot the clean tree.
	cp, err := app.CreateCheckpoint(repo, "1", "turn 1")
	if err != nil {
		t.Fatal(err)
	}
	if cp.ID == 0 {
		t.Fatal("expected a checkpoint for the initial state")
	}

	// The agent edits a tracked file and creates a brand-new (untracked) one.
	write("tracked.txt", "v2\n")
	write("created.txt", "new\n")

	diff, err := app.CheckpointDiff(repo, cp.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "created.txt") || !strings.Contains(diff, "v2") {
		t.Fatalf("diff missed the turn's changes:\n%s", diff)
	}

	if _, err := app.RestoreCheckpoint(repo, cp.Commit); err != nil {
		t.Fatal(err)
	}
	if got := read("tracked.txt"); got != "v1\n" {
		t.Fatalf("tracked file not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "created.txt")); !os.IsNotExist(err) {
		t.Fatal("file created after the checkpoint survived the restore")
	}

	// The restore itself is undoable: the pre-restore state was checkpointed.
	list, err := app.ListCheckpoints(repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Label != "before restore" {
		t.Fatalf("expected a safety checkpoint on top, got %+v", list)
	}
	if _, err := app.RestoreCheckpoint(repo, list[0].Commit); err != nil {
		t.Fatal(err)
	}
	if got := read("tracked.txt"); got != "v2\n" {
		t.Fatalf("undo of the restore failed: %q", got)
	}
	if got := read("created.txt"); got != "new\n" {
		t.Fatalf("untracked file not brought back: %q", got)
	}
}

// An unchanged tree must not pile up identical checkpoints.
func TestCheckpointSkipsUnchangedTree(t *testing.T) {
	repo := t.TempDir()
	runGitEnv(repo, nil, "init", "-q")
	runGitEnv(repo, nil, "config", "user.email", "t@t")
	runGitEnv(repo, nil, "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x"), 0o644)
	runGitEnv(repo, nil, "add", ".")
	runGitEnv(repo, nil, "commit", "-qm", "init")

	app := &App{db: mustTestDB(t)}
	if cp, err := app.CreateCheckpoint(repo, "1", "first"); err != nil || cp.ID == 0 {
		t.Fatalf("first checkpoint: %+v %v", cp, err)
	}
	cp, err := app.CreateCheckpoint(repo, "1", "second")
	if err != nil {
		t.Fatal(err)
	}
	if cp.ID != 0 {
		t.Fatal("identical tree should not create a second checkpoint")
	}
}

func TestTurnAuditFreezesDiffOnSettlement(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out := runGitEnv(repo, nil, args...); !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "init")

	app := &App{db: mustTestDB(t)}
	audit, err := app.StartTurnAudit(repo, "chat:42", "change app")
	if err != nil || audit.ID == 0 || audit.Checkpoint == "" {
		t.Fatalf("start audit = %+v, %v", audit, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settled, err := app.SettleTurnAudit("chat:42", "done")
	if err != nil {
		t.Fatal(err)
	}
	if settled.State != "done" || settled.SettledAt == 0 || !strings.Contains(settled.Diff, "after") || len(settled.Files) != 1 || settled.Files[0] != "app.txt" {
		t.Fatalf("settled audit = %+v", settled)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	frozen, err := app.CheckpointDiff(repo, audit.Checkpoint)
	if err != nil || !strings.Contains(frozen, "after") || strings.Contains(frozen, "later") {
		t.Fatalf("history diff must be frozen at settlement, got %q (%v)", frozen, err)
	}
}

// A plain directory is not an error — it just has no history.
func TestCheckpointNonRepoIsNoop(t *testing.T) {
	app := &App{db: mustTestDB(t)}
	cp, err := app.CreateCheckpoint(t.TempDir(), "1", "x")
	if err != nil || cp.ID != 0 {
		t.Fatalf("non-repo should be a silent no-op, got %+v %v", cp, err)
	}
}

func mustTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// The snapshot seeds from a copy of the real index (for its stat cache). It
// must still capture the worktree, not what happens to be staged, and leave
// the real index alone.
func TestSnapshotTreeIgnoresStagingState(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out := runGitEnv(repo, nil, args...)
		if !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
		return strings.TrimSpace(out.Stdout)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	write("a.txt", "v1\n")
	write("gone.txt", "x\n")
	git("add", ".")
	git("commit", "-qm", "init")

	write("a.txt", "staged\n")
	git("add", "a.txt")
	write("a.txt", "worktree\n") // staged content differs from the worktree
	git("rm", "-q", "--cached", "gone.txt")
	write("new.txt", "n\n")
	stagedBefore := git("diff", "--cached", "--name-status")

	tree, err := snapshotTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := git("show", tree+":a.txt"); got != "worktree" {
		t.Fatalf("a.txt = %q, want worktree content", got)
	}
	for _, f := range []string{"gone.txt", "new.txt"} { // on disk ⇒ in the snapshot
		if out := runGitEnv(repo, nil, "cat-file", "-e", tree+":"+f); !out.Success {
			t.Fatalf("%s missing from snapshot", f)
		}
	}
	if after := git("diff", "--cached", "--name-status"); after != stagedBefore {
		t.Fatalf("real index changed:\n%s\nwant\n%s", after, stagedBefore)
	}
}

// Racy git: an edit with the same size inside the same second as the indexed
// version is stat-identical to it. Git only rechecks such an entry because the
// file is not older than the index — so the snapshot's index copy must keep
// the real index's mtime. Pin every timestamp to one instant so this fails
// deterministically when it doesn't (it used to flake TestCheckpointRoundTrip).
func TestSnapshotTreeSeesSameSizeSameSecondEdit(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out := runGitEnv(repo, nil, args...)
		if !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
		return strings.TrimSpace(out.Stdout)
	}
	file := filepath.Join(repo, "tracked.txt")
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("config", "core.trustctime", "false") // ctime can't be pinned
	if err := os.WriteFile(file, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "init")

	// Stamp the index entry for v1 with a pinned mtime, then overwrite with v2
	// (same size) and pin file and index to that same instant: v2 is now
	// stat-identical to the entry, exactly like a real same-second write.
	when := time.Now().Add(-time.Hour).Truncate(time.Second)
	pin := func(paths ...string) {
		t.Helper()
		for _, p := range paths {
			if err := os.Chtimes(p, when, when); err != nil {
				t.Fatal(err)
			}
		}
	}
	pin(file)
	git("update-index", "--refresh")
	if err := os.WriteFile(file, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pin(file, filepath.Join(repo, ".git", "index"))

	tree, err := snapshotTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := git("cat-file", "-p", tree+":tracked.txt"); got != "v2" {
		t.Fatalf("snapshot kept the stale content: %q", got)
	}
}
