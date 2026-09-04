package main

import (
	"errors"
	"os/exec"
)

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
	ws, err := a.CreateWorkspace(worktreeName, path)
	if err != nil {
		return ws, err
	}
	_, err = a.db.Exec(`UPDATE workspaces SET parent_id = ?, worktree_branch = ?, is_git = 1 WHERE id = ?`, parentID, branch, ws.ID)
	ws.ParentID = &parentID
	ws.WorktreeBranch = &branch
	ws.IsGit = true
	return ws, err
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

func gitErr(out GitOutput) error {
	if out.Stderr == "" {
		return errors.New("git command failed")
	}
	return errors.New(out.Stderr)
}
