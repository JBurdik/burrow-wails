package main

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"strings"
	"time"
)

// What happens around `git worktree add` (CreateWorktree): which ref a new
// worktree starts from, and the repo's setup scripts that run in it afterwards.
// CreateWorktree is the one door every creator goes through (Sidebar dialog,
// WelcomeScreen, the create_worktree / move_to_worktree verbs), so both live
// here rather than in each caller.

const worktreeFetchTimeout = 20 * time.Second

// worktreeFromOrigin reads Settings → General "Start new worktrees from
// origin" (uiPrefs.worktreeFromOrigin) fresh. Absent = on, like the toggle's
// default; an unreadable config = off (branch locally, as before the setting).
func (a *App) worktreeFromOrigin() bool {
	raw, err := a.ReadConfig()
	if err != nil {
		return false
	}
	var cfg struct {
		UI struct {
			FromOrigin *bool `json:"worktreeFromOrigin"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return false
	}
	return cfg.UI.FromOrigin == nil || *cfg.UI.FromOrigin
}

// resolveWorktreeBase turns base B into the ref a new worktree branches from:
// origin/B after a successful `git fetch origin B`, else B itself. warning is
// non-empty when the fetch was tried and failed — the worktree is still made,
// from the local branch. HEAD, a repo without an origin remote, or fromOrigin
// off all mean "local" with no warning. A base already named origin/B fetches
// B; with the setting off it is used as given.
func resolveWorktreeBase(repoPath, base string, fromOrigin bool) (ref, warning string) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "HEAD"
	}
	// A full commit id (ForgePrCheckout's fetched PR head) is already exact.
	if !fromOrigin || base == "HEAD" || isFullSHA(base) {
		return base, ""
	}
	if !runCmd("git", repoPath, []string{"remote", "get-url", "origin"}).Success {
		return base, ""
	}
	branch := strings.TrimPrefix(base, "origin/")
	ctx, cancel := context.WithTimeout(context.Background(), worktreeFetchTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "fetch", "--quiet", "origin", branch)
	c.Dir = repoPath
	// Never block on a credential prompt nobody can see.
	c.Env = append(c.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := c.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if ctx.Err() != nil {
			msg = "timed out after " + worktreeFetchTimeout.String()
		} else if msg == "" {
			msg = err.Error()
		}
		return base, "git fetch origin " + branch + " failed (" + msg + "); branched from local " + base
	}
	remoteRef := "origin/" + branch
	if !runCmd("git", repoPath, []string{"rev-parse", "--verify", "--quiet", remoteRef + "^{commit}"}).Success {
		return base, remoteRef + " not found after fetch; branched from local " + base
	}
	return remoteRef, ""
}

// startWorktreeSetup hands the new worktree to the frontend, which runs the
// repo's "Run when a worktree is created" scripts in a Right Panel terminal
// there (the scripts live in <repo>/.burrow/config.toml, parsed by
// src/stores/scripts.ts) and shows warning as a toast. Fire-and-forget: the
// worktree exists either way, and an app with no window just skips setup.
func (a *App) startWorktreeSetup(workspaceID int64, repoPath, path, warning string) {
	if warning != "" {
		log.Printf("[worktree] %s", warning)
	}
	if a.ui == nil {
		return
	}
	go func() {
		_, err := a.ui.Do(context.Background(), "worktree_setup", map[string]any{
			"workspaceId": workspaceID, "repoPath": repoPath, "path": path, "warning": warning,
		})
		if err != nil {
			log.Printf("[worktree] setup for %s: %v", path, err)
		}
	}()
}

func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
