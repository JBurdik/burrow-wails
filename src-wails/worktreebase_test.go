package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A clone whose origin has moved on: from origin, the new worktree starts at
// origin's tip; with origin unreachable, it falls back to the local branch
// and says so instead of failing.
func TestResolveWorktreeBase(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	gitT(t, root, "init", "--bare", "-b", "main", bare)
	seed := filepath.Join(root, "seed")
	gitT(t, root, "clone", bare, seed)
	gitT(t, seed, "commit", "--allow-empty", "-m", "one")
	gitT(t, seed, "push", "origin", "HEAD:main")
	repo := filepath.Join(root, "repo")
	gitT(t, root, "clone", bare, repo)
	gitT(t, seed, "commit", "--allow-empty", "-m", "two")
	gitT(t, seed, "push", "origin", "HEAD:main")
	tip := gitT(t, seed, "rev-parse", "HEAD")

	ref, warn := resolveWorktreeBase(repo, "main", true)
	if ref != "origin/main" || warn != "" {
		t.Fatalf("fetch ok: got %q warn=%q", ref, warn)
	}
	if got := gitT(t, repo, "rev-parse", ref); got != tip {
		t.Fatalf("origin/main = %s, want fetched tip %s", got, tip)
	}

	if ref, warn := resolveWorktreeBase(repo, "main", false); ref != "main" || warn != "" {
		t.Fatalf("setting off: got %q warn=%q", ref, warn)
	}
	if ref, warn := resolveWorktreeBase(repo, "", true); ref != "HEAD" || warn != "" {
		t.Fatalf("no base: got %q warn=%q", ref, warn)
	}

	if err := os.RemoveAll(bare); err != nil {
		t.Fatal(err)
	}
	ref, warn = resolveWorktreeBase(repo, "main", true)
	if ref != "main" || warn == "" {
		t.Fatalf("fetch failed: got %q warn=%q, want local fallback with a warning", ref, warn)
	}
}
