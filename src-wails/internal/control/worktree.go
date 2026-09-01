package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worktree resolution. A caller says "make me a worktree for branch X" from
// wherever it happens to be running — possibly already inside a worktree — so
// these two verbs do the climbing and path arithmetic the frontend used to do.

// repoOf resolves the caller's cwd to the ROOT repo workspace: a worktree's row
// carries parent_id, and git has no worktree-of-a-worktree, so we always climb.
func (c *Core) repoOf(cwd string) (id int64, path string, err error) {
	if cwd == "" {
		return 0, "", fmt.Errorf("no cwd: pass one explicitly")
	}
	var parent *int64
	if err := c.deps.DB.QueryRow(`SELECT id, path, parent_id FROM workspaces WHERE path = ?`, cwd).
		Scan(&id, &path, &parent); err != nil {
		return 0, "", fmt.Errorf("%s is not a workspace", cwd)
	}
	if parent != nil {
		if err := c.deps.DB.QueryRow(`SELECT id, path FROM workspaces WHERE id = ?`, *parent).
			Scan(&id, &path); err != nil {
			return 0, "", fmt.Errorf("parent workspace %d is gone", *parent)
		}
	}
	return id, path, nil
}

func (c *Core) createWorktree(ctx context.Context, p Params) (any, error) {
	branch := p.Str("branch")
	_, repoPath, err := c.repoOf(p.Str("cwd"))
	if err != nil {
		return nil, fmt.Errorf("create_worktree: %w", err)
	}

	path := p.Str("path")
	if path == "" {
		root := "~/burrow-worktrees"
		if c.deps.WorktreesDir != nil {
			if d := strings.TrimSpace(c.deps.WorktreesDir()); d != "" {
				root = d
			}
		}
		path = filepath.Join(expandHome(root), filepath.Base(repoPath), branch)
	}
	path = expandHome(path)

	id, err := c.deps.Worktrees.Create(repoPath, branch, path, branch, p.Str("base"))
	if err != nil {
		return nil, fmt.Errorf("create_worktree: %w", err)
	}
	// Surface it immediately: the frontend has to reload its workspace list for
	// the worktree to appear nested under its repo (and to be able to host tabs).
	// A UI that can't be reached is not fatal — the worktree exists either way.
	uiErr := ""
	if err := c.ui(ctx, "focus_workspace", map[string]any{"workspaceId": id}, nil); err != nil {
		uiErr = err.Error()
	}
	return map[string]any{"workspace_id": id, "path": path, "branch": branch, "ui_error": uiErr}, nil
}

func (c *Core) removeWorktree(ctx context.Context, p Params) (any, error) {
	branch, wtPath := p.Str("branch"), expandHome(p.Str("path"))
	if branch == "" && wtPath == "" {
		return nil, fmt.Errorf("worktree_remove: pass branch or path")
	}
	repoID, _, err := c.repoOf(p.Str("cwd"))
	if err != nil {
		return nil, fmt.Errorf("worktree_remove: %w", err)
	}

	// Scoped to this repo's own worktrees: a Manager is anchored to one repo, and
	// removing another project's worktree by a same-named branch would be a
	// spectacular way to lose work.
	var id int64
	var target string
	if branch != "" {
		err = c.deps.DB.QueryRow(
			`SELECT id, path FROM workspaces WHERE parent_id = ? AND worktree_branch = ?`, repoID, branch).
			Scan(&id, &target)
	} else {
		err = c.deps.DB.QueryRow(
			`SELECT id, path FROM workspaces WHERE parent_id = ? AND path = ?`, repoID, wtPath).
			Scan(&id, &target)
	}
	if err != nil {
		return nil, fmt.Errorf("worktree_remove: no worktree of this repo matches %s%s", branch, wtPath)
	}
	if err := c.deps.Worktrees.Remove(id, p.Bool("force")); err != nil {
		return nil, fmt.Errorf("worktree_remove: %w", err)
	}
	uiErr := ""
	if err := c.ui(ctx, "workspaces_reload", nil, nil); err != nil {
		uiErr = err.Error()
	}
	return map[string]any{"removed": target, "ui_error": uiErr}, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
