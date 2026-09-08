package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// expandHome resolves a leading "~/" the way a shell would — exec.Command
// never goes through one, so a literal "~/burrow-worktrees" (the default
// worktreesDir) would otherwise land as a directory named "~" inside the
// repo itself instead of the user's home.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

// GitOutput mirrors the Rust struct returned by run_git/run_gh.
type GitOutput struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Code    int    `json:"code"`
	Success bool   `json:"success"`
}

func runCmd(name, cwd string, args []string) GitOutput {
	c := exec.Command(name, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	var stdout, stderr []byte
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

func (a *App) RunGit(cwd string, args []string) GitOutput {
	return runCmd("git", cwd, args)
}

func (a *App) RunGh(cwd string, args []string) GitOutput {
	bin := "gh"
	if resolved := resolveAgentBin("gh", ""); resolved != "" {
		bin = resolved
	}
	return runCmd(bin, cwd, args)
}

// --- worktrees ---

func (a *App) CreateWorktree(repoPath, worktreeName, path, branch, baseRef string) (Workspace, error) {
	path = expandHome(path)
	var parentID int64
	if err := a.db.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, repoPath).Scan(&parentID); err != nil {
		return Workspace{}, err
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	args := []string{"worktree", "add", path, "-b", branch, baseRef}
	if out := runCmd("git", repoPath, args); !out.Success {
		// Branch may already exist — retry without -b.
		args = []string{"worktree", "add", path, branch}
		if out2 := runCmd("git", repoPath, args); !out2.Success {
			return Workspace{}, gitErr(out2)
		}
	}
	// CreateWorkspace already emits workspaces-changed once the row exists,
	// but AT THAT POINT the row isn't nested yet (parent_id/worktree_branch/
	// is_git below are still unset) — a client reloading on that first emit
	// can see the worktree as a top-level non-git workspace, and nothing
	// corrects it until an unrelated mutation happens to fire another emit.
	// This is the only write to the workspaces table outside workspace.go,
	// so it is also the one hole in "every mutation of the list notifies".
	// Fixed by emitting again below, once the row is actually nested. The
	// inner emit stays: CreateWorkspace is shared with the plain
	// (non-worktree) creation path, and duplicating its body here just to
	// suppress one harmless, idempotent extra event isn't worth the coupling
	// — a client that reloads on the premature emit and reloads again on the
	// correct one ends up in the same place either way.
	ws, err := a.CreateWorkspace(worktreeName, path)
	if err != nil {
		return ws, err
	}
	_, err = a.db.Exec(`UPDATE workspaces SET parent_id = ?, worktree_branch = ?, is_git = 1 WHERE id = ?`, parentID, branch, ws.ID)
	ws.ParentID = &parentID
	ws.WorktreeBranch = &branch
	ws.IsGit = true
	if err == nil {
		emitWorkspacesChanged()
	}
	return ws, err
}

// RenameWorktreeBranch renames a worktree's branch in place (git branch -m —
// the worktree's own directory keeps its original path, same as t3code:
// nothing depends on the path encoding the branch name once it exists).
// Callers use this to rename the throwaway hex branch a worktree is created
// with to a task-derived name generated afterward, off the critical path of
// actually opening the new worktree.
func (a *App) RenameWorktreeBranch(id int64, oldBranch, newBranch string) (Workspace, error) {
	var wsPath string
	if err := a.db.QueryRow(`SELECT path FROM workspaces WHERE id = ?`, id).Scan(&wsPath); err != nil {
		return Workspace{}, err
	}
	if out := runCmd("git", wsPath, []string{"branch", "-m", oldBranch, newBranch}); !out.Success {
		return Workspace{}, gitErr(out)
	}
	if _, err := a.db.Exec(`UPDATE workspaces SET worktree_branch = ? WHERE id = ?`, newBranch, id); err != nil {
		return Workspace{}, err
	}
	emitWorkspacesChanged()
	row := a.db.QueryRow("SELECT "+workspaceCols+" FROM workspaces WHERE id = ?", id)
	return scanWorkspace(row)
}

func (a *App) RemoveWorktree(id int64, force bool) error {
	var path, parentPath string
	err := a.db.QueryRow(`SELECT w.path, p.path FROM workspaces w LEFT JOIN workspaces p ON p.id = w.parent_id WHERE w.id = ?`, id).Scan(&path, &parentPath)
	if err != nil {
		return err
	}
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	if out := runCmd("git", parentPath, args); !out.Success {
		return gitErr(out)
	}
	return a.DeleteWorkspace(id)
}

// --- branch diff (against the repo's default/upstream branch) ---

// branchDiffBase finds the branch "Branch changes" should diff against.
// No repo config carries a "default branch" anywhere else in this codebase,
// so this tries the remote's advertised HEAD first, then falls back to a
// local main/master. Returns "" when neither exists (a repo with no remote
// and no conventionally-named branch), which the caller treats as "ask the
// user to pick one" rather than as an error.
func branchDiffBase(cwd string) string {
	if out := runCmd("git", cwd, []string{"symbolic-ref", "refs/remotes/origin/HEAD"}); out.Success {
		ref := strings.TrimSpace(out.Stdout)
		if i := strings.LastIndex(ref, "/"); i >= 0 {
			return ref[i+1:]
		}
	}
	for _, name := range []string{"main", "master"} {
		if runCmd("git", cwd, []string{"show-ref", "--verify", "--quiet", "refs/heads/" + name}).Success {
			return name
		}
	}
	return ""
}

func (a *App) BranchDiffBase(cwd string) string {
	return branchDiffBase(cwd)
}

// BranchDiff returns what changed on the current branch since it diverged
// from branchDiffBase, i.e. "git diff <base>...HEAD". Empty base means no
// base could be determined; empty result means base found but no changes.
func (a *App) BranchDiff(cwd string) (string, error) {
	base := branchDiffBase(cwd)
	if base == "" {
		return "", nil
	}
	out := runCmd("git", cwd, []string{"diff", base + "...HEAD"})
	if !out.Success {
		return "", gitErr(out)
	}
	return out.Stdout, nil
}

func gitErr(out GitOutput) error {
	if out.Stderr == "" {
		return errors.New("git command failed")
	}
	return errors.New(out.Stderr)
}
