package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
