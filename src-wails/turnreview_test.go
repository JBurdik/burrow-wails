package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastTurnAuditIsScopedAndSkipsRunningTurns(t *testing.T) {
	app := &App{db: mustTestDB(t)}
	for _, row := range []struct {
		cwd, subject, diff string
		settled            int64
	}{
		{"/repo", "chat:42", "old", 1},
		{"/repo", "chat:42", "frozen", 2},
		{"/repo", "chat:42", "running", 0},
		{"/repo", "chat:43", "another chat", 3},
		{"/other", "chat:42", "another repo", 4},
	} {
		_, err := app.db.Exec(`INSERT INTO turn_audits (cwd, subject_id, started_at, settled_at, diff) VALUES (?, ?, 1, ?, ?)`, row.cwd, row.subject, row.settled, row.diff)
		if err != nil {
			t.Fatal(err)
		}
	}
	audit, err := app.LastTurnAudit("/repo", "chat:42")
	if err != nil || audit == nil || audit.Diff != "frozen" {
		t.Fatalf("last = %+v, %v", audit, err)
	}
	empty, err := app.LastTurnAudit("/repo", "chat:missing")
	if err != nil || empty != nil {
		t.Fatalf("missing = %+v, %v", empty, err)
	}
}

func TestWorkingTreeDiffIncludesNewFilesAndKeepsIndex(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out := runGitEnv(repo, nil, args...)
		if !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
		return out.Stdout
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	write("app.txt", "before\n")
	git("add", ".")
	git("commit", "-qm", "initial")
	write("app.txt", "staged\n")
	git("add", "app.txt")
	write("app.txt", "final\n")
	write("new.txt", "new file\n")
	beforeIndex := git("diff", "--cached")
	diff, err := (&App{}).WorkingTreeDiff(repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(diff, "diff --git a/app.txt") != 1 || !strings.Contains(diff, "+final") || !strings.Contains(diff, "+new file") {
		t.Fatalf("unexpected diff: %s", diff)
	}
	if git("diff", "--cached") != beforeIndex {
		t.Fatal("review changed the real index")
	}
	if git("show", "HEAD:app.txt") != "before\n" {
		t.Fatal("review changed HEAD")
	}
}

func TestWorkingTreeDiffBeforeFirstCommit(t *testing.T) {
	repo := t.TempDir()
	if out := runGitEnv(repo, nil, "init", "-q"); !out.Success {
		t.Fatal(out.Stderr)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := (&App{}).WorkingTreeDiff(repo)
	if err != nil || !strings.Contains(diff, "+first") {
		t.Fatalf("diff = %q, %v", diff, err)
	}
}
