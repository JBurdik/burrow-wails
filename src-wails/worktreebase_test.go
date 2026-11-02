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

// Both fetches the app runs unattended — the worktree base and a PR head —
// must refuse credential prompts. A fake `git` on PATH records the env each
// `fetch` saw and fails it; everything else succeeds.
func TestBackgroundFetchesNeverPrompt(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "fetch.env")
	script := "#!/bin/sh\n[ \"$1\" = fetch ] || exit 0\nenv >> " + log + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_TERMINAL_PROMPT", "1") // an inherited value must not win
	t.Setenv("GCM_INTERACTIVE", "always")
	repo := t.TempDir()

	check := func(door string) {
		t.Helper()
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("%s: fetch never ran: %v", door, err)
		}
		lines := strings.Split(string(b), "\n")
		last := func(key string) string {
			v := ""
			for _, l := range lines {
				if strings.HasPrefix(l, key+"=") {
					v = strings.TrimPrefix(l, key+"=")
				}
			}
			return v
		}
		if got := last("GIT_TERMINAL_PROMPT"); got != "0" {
			t.Errorf("%s: GIT_TERMINAL_PROMPT=%q, want 0", door, got)
		}
		if got := last("GCM_INTERACTIVE"); got != "never" {
			t.Errorf("%s: GCM_INTERACTIVE=%q, want never", door, got)
		}
		os.Remove(log)
	}

	if _, warn := resolveWorktreeBase(repo, "main", true); warn == "" {
		t.Fatal("expected the failed fetch to warn")
	}
	check("worktree base")

	a := newTestApp(t)
	ws, err := a.CreateWorkspace("repo", repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetForgeProvider(ws.ID, "github"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ForgePrCheckout(repo, 7, "pr-7", filepath.Join(repo, "wt")); err == nil {
		t.Fatal("expected the failed fetch to fail the checkout")
	}
	check("PR head")
}
