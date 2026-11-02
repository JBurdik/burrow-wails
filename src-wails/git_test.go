package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// branchDiffBase has three cases: origin/HEAD advertised, no remote but a
// conventionally-named local branch, and neither — the last one is what
// "Branch changes" in the RightPanel treats as "ask the user to pick one".
func TestBranchDiffBaseFallback(t *testing.T) {
	git := func(cwd string, args ...string) {
		t.Helper()
		if out := runCmd("git", cwd, args); !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	commit := func(cwd string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(cwd, "add", ".")
		git(cwd, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "c")
	}

	t.Run("no remote, no main/master", func(t *testing.T) {
		repo := t.TempDir()
		git(repo, "init", "-q", "-b", "trunk")
		commit(repo)
		if got := branchDiffBase(repo); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})

	t.Run("local main, no remote", func(t *testing.T) {
		repo := t.TempDir()
		git(repo, "init", "-q", "-b", "main")
		commit(repo)
		if got := branchDiffBase(repo); got != "main" {
			t.Fatalf("want main, got %q", got)
		}
	})

	t.Run("origin/HEAD wins over local main", func(t *testing.T) {
		upstream := t.TempDir()
		git(upstream, "init", "-q", "--bare", "-b", "release")

		repo := t.TempDir()
		git(repo, "init", "-q", "-b", "main")
		commit(repo)
		git(repo, "remote", "add", "origin", upstream)
		git(repo, "push", "-q", "origin", "main")
		git(repo, "push", "-q", "origin", "main:release")
		git(repo, "remote", "set-head", "origin", "release")

		if got := branchDiffBase(repo); got != "release" {
			t.Fatalf("want release, got %q", got)
		}
	})
}

// Hunk staging depends on stdin reaching `git apply --cached -`.
func TestRunGitInputPipesStdin(t *testing.T) {
	dir := t.TempDir()
	a := &App{}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out := a.RunGit(dir, args); out.Code != 0 {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.RunGit(dir, []string{"add", "f.txt"})
	a.RunGit(dir, []string{"commit", "-qm", "init"})
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := a.RunGit(dir, []string{"diff"}).Stdout
	if out := a.RunGitInput(dir, []string{"apply", "--cached", "-"}, patch); out.Code != 0 {
		t.Fatalf("apply: %s", out.Stderr)
	}
	if got := a.RunGit(dir, []string{"diff", "--cached", "--name-only"}).Stdout; strings.TrimSpace(got) != "f.txt" {
		t.Fatalf("staged = %q", got)
	}
}
