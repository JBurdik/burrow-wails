package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Worktree cleanup: an hourly sweep that removes worktrees nobody needs any
// more, by rule (Settings → General, uiPrefs, read fresh each run like
// chatAutoSleepMinutes; every rule is opt-in):
//
//	worktreeCleanupMerged        — its branch landed in the repo's default branch
//	worktreeCleanupInactive      — no chats, no tabs, no activity for N days
//	worktreeCleanupInactiveDays  — N (default 14, 0 = rule off)
//
// The protections in worktreeProtected beat every rule. Removal is the same
// path as the sidebar's: removeWorktree (non-forced, under the tree's lock)
// then DeleteWorkspace. The branch is kept, except a merged one, which goes
// too. Preview/"Clean up now" evaluate both rules whether or not they are on,
// so a user can see what opting in would do before doing it.
// ponytail: ported from t3code's storageCleanup.ts minus its per-project
// overrides, settled-thread retention and deleted-thread rule.

const (
	worktreeCleanupInterval    = time.Hour
	worktreeCleanupGrace       = time.Hour // the row can exist before its chats do
	defaultWorktreeInactiveDay = 14
)

type worktreeCleanupRules struct {
	Merged       bool
	InactiveDays int // 0 = off
}

// worktreeFacts is everything the verdict needs, gathered by the caller.
type worktreeFacts struct {
	Created      time.Time
	LastActivity time.Time // newest of created, last opened, any chat's activity
	Dirty        bool      // modified or untracked files, or status unreadable
	LiveProcess  bool      // a chat in it has a running agent process
	OpenPty      bool      // a tab in it has a live PTY
	Active       bool      // the desktop's active workspace
	Pinned       bool      // one of its chats is pinned in the sidebar
	Chats        int       // unarchived chats
	Tabs         int       // saved tab rows, live or not
	Merged       bool
}

// worktreeProtected names the reason a worktree must stay, "" if none.
func worktreeProtected(f worktreeFacts, now time.Time) string {
	switch {
	case now.Sub(f.Created) < worktreeCleanupGrace:
		return "created less than an hour ago"
	case f.Active:
		return "active workspace"
	case f.LiveProcess:
		return "a chat is running"
	case f.OpenPty:
		return "a terminal is open"
	case f.Pinned:
		return "has a pinned chat"
	case f.Dirty:
		return "uncommitted or untracked files"
	}
	return ""
}

// worktreeCleanupVerdict is the whole decision: which rule removes the
// worktree ("merged" | "inactive") and why, or "" to keep it.
func worktreeCleanupVerdict(f worktreeFacts, r worktreeCleanupRules, now time.Time) (rule, reason string) {
	if worktreeProtected(f, now) != "" {
		return "", ""
	}
	if r.Merged && f.Merged {
		return "merged", "branch was merged"
	}
	if r.InactiveDays > 0 && f.Chats == 0 && f.Tabs == 0 {
		if days := int(now.Sub(f.LastActivity) / (24 * time.Hour)); days >= r.InactiveDays {
			return "inactive", fmt.Sprintf("no chats, no tabs, inactive for %d days", days)
		}
	}
	return "", ""
}

// WorktreeCleanupItem is one worktree the cleanup would remove (or did).
type WorktreeCleanupItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
	parent string
}

type worktreeCleanupConfig struct {
	auto, preview worktreeCleanupRules
	activeID      int64
	pinned        map[int64]bool
}

// worktreeCleanupSettings reads config.json fresh. An unreadable config is an
// error: the sweep never removes on a guess about what is active or pinned.
func (a *App) worktreeCleanupSettings() (worktreeCleanupConfig, error) {
	raw, err := a.ReadConfig()
	if err != nil {
		return worktreeCleanupConfig{}, err
	}
	var cfg struct {
		UI struct {
			Merged   bool `json:"worktreeCleanupMerged"`
			Inactive bool `json:"worktreeCleanupInactive"`
			Days     *int `json:"worktreeCleanupInactiveDays"`
		} `json:"uiPrefs"`
		Open struct {
			ActiveID *int64 `json:"activeId"`
		} `json:"burrow.open_workspaces"`
		PinnedChats []int64 `json:"sidebarPinnedChats"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return worktreeCleanupConfig{}, err
	}
	days := defaultWorktreeInactiveDay
	if cfg.UI.Days != nil {
		days = *cfg.UI.Days
	}
	c := worktreeCleanupConfig{
		preview: worktreeCleanupRules{Merged: true, InactiveDays: days},
		auto:    worktreeCleanupRules{Merged: cfg.UI.Merged},
		pinned:  map[int64]bool{},
	}
	if cfg.UI.Inactive {
		c.auto.InactiveDays = days
	}
	if cfg.Open.ActiveID != nil {
		c.activeID = *cfg.Open.ActiveID
	}
	for _, id := range cfg.PinnedChats {
		c.pinned[id] = true
	}
	return c, nil
}

// worktreeCleanupCandidates gathers facts for every worktree and returns the
// ones a rule removes. Expensive checks (git status, fetch, forge) only run
// for worktrees the cheap protections did not already keep. only != nil
// restricts it to those ids.
func (a *App) worktreeCleanupCandidates(c worktreeCleanupConfig, r worktreeCleanupRules, now time.Time, only map[int64]bool) ([]WorktreeCleanupItem, error) {
	if !r.Merged && r.InactiveDays <= 0 {
		return nil, nil
	}
	rows, err := a.db.Query(`SELECT w.id, w.name, w.path, COALESCE(w.worktree_branch, ''), w.created_at,
		COALESCE(w.last_opened, 0), p.path FROM workspaces w JOIN workspaces p ON p.id = w.parent_id`)
	if err != nil {
		return nil, err
	}
	var all []WorktreeCleanupItem
	created := map[int64]int64{}
	opened := map[int64]int64{}
	for rows.Next() {
		var it WorktreeCleanupItem
		var c, o int64
		if err := rows.Scan(&it.ID, &it.Name, &it.Path, &it.Branch, &c, &o, &it.parent); err != nil {
			rows.Close()
			return nil, err
		}
		if only == nil || only[it.ID] {
			all = append(all, it)
			created[it.ID], opened[it.ID] = c, o
		}
	}
	rows.Close()

	var live map[string]bool // nil = unknown → every tab counts as a live PTY
	if a.daemon != nil {
		if ids, err := a.daemon.List(); err == nil {
			live = map[string]bool{}
			for _, id := range ids {
				live[id] = true
			}
		}
	}
	fetched := map[string]string{} // parent path → default branch, fetched once per run

	var out []WorktreeCleanupItem
	for _, it := range all {
		f := worktreeFacts{
			Created:      time.UnixMilli(created[it.ID]),
			LastActivity: time.UnixMilli(max(created[it.ID], opened[it.ID])),
			Active:       it.ID == c.activeID,
		}
		if err := a.worktreeChatFacts(it.ID, c.pinned, &f); err != nil {
			return nil, err
		}
		if err := a.worktreeTabFacts(it.ID, live, &f); err != nil {
			return nil, err
		}
		if worktreeProtected(f, now) != "" {
			continue
		}
		st := runCmd("git", it.Path, []string{"-c", "status.showUntrackedFiles=normal", "status", "--porcelain"})
		f.Dirty = !st.Success || strings.TrimSpace(st.Stdout) != ""
		if r.Merged && !f.Dirty && it.Branch != "" {
			def, ok := fetched[it.parent]
			if !ok {
				if def = branchDiffBase(it.parent); def != "" {
					runCmd("git", it.parent, []string{"fetch", "--quiet", "origin", def})
				}
				fetched[it.parent] = def
			}
			f.Merged = a.worktreeBranchMerged(it.Path, it.parent, it.Branch, def)
		}
		if it.Rule, it.Reason = worktreeCleanupVerdict(f, r, now); it.Rule != "" {
			out = append(out, it)
		}
	}
	return out, nil
}

func (a *App) worktreeChatFacts(wsID int64, pinned map[int64]bool, f *worktreeFacts) error {
	rows, err := a.db.Query(`SELECT id, archived_at, last_activity_at FROM chats WHERE workspace_id = ?`, wsID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, archived, act int64
		if err := rows.Scan(&id, &archived, &act); err != nil {
			return err
		}
		if archived == 0 {
			f.Chats++
		}
		if t := time.UnixMilli(act); t.After(f.LastActivity) {
			f.LastActivity = t
		}
		f.Pinned = f.Pinned || pinned[id]
		key := strconv.FormatInt(id, 10)
		if a.claudeMgr().Alive(key) {
			f.LiveProcess = true
		} else if _, ok := a.acpReg().get(key); ok {
			f.LiveProcess = true
		}
	}
	return rows.Err()
}

func (a *App) worktreeTabFacts(wsID int64, live map[string]bool, f *worktreeFacts) error {
	rows, err := a.db.Query(`SELECT pty_id FROM terminal_tabs WHERE workspace_id = ?`, wsID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pty *int64
		if err := rows.Scan(&pty); err != nil {
			return err
		}
		f.Tabs++
		if live == nil || (pty != nil && live[strconv.FormatInt(*pty, 10)]) {
			f.OpenPty = true
		}
	}
	return rows.Err()
}

// worktreeBranchMerged: the branch tip is in origin/<default> and the branch
// was pushed (a never-pushed branch with no commits of its own is also an
// ancestor — that is a fresh worktree, not a merged one), or the forge says
// the branch's PR merged (squash/rebase) and every commit on it is on a
// remote, so nothing local-only goes with it.
func (a *App) worktreeBranchMerged(path, parent, branch, def string) bool {
	if def != "" &&
		runCmd("git", parent, []string{"config", "--get", "branch." + branch + ".remote"}).Success &&
		runCmd("git", parent, []string{"merge-base", "--is-ancestor", "refs/heads/" + branch, "refs/remotes/origin/" + def}).Success {
		return true
	}
	f, err := a.resolveForge(path)
	if err != nil {
		return false
	}
	pr, err := f.View(path, 0)
	if err != nil || pr.State != "merged" || pr.HeadRef != branch {
		return false
	}
	out := runCmd("git", parent, []string{"rev-list", "--count", "refs/heads/" + branch, "--not", "--remotes"})
	return out.Success && strings.TrimSpace(out.Stdout) == "0"
}

var worktreeCleanupMu sync.Mutex // one run at a time (sweep vs. "Clean up now")

// cleanupWorktrees removes every candidate, logs each removal and emits one
// worktrees-cleaned summary for the toast.
func (a *App) cleanupWorktrees(c worktreeCleanupConfig, r worktreeCleanupRules, now time.Time, only map[int64]bool) ([]WorktreeCleanupItem, error) {
	worktreeCleanupMu.Lock()
	defer worktreeCleanupMu.Unlock()
	items, err := a.worktreeCleanupCandidates(c, r, now, only)
	if err != nil {
		return nil, err
	}
	removed := []WorktreeCleanupItem{}
	for _, it := range items {
		if err := removeWorktree(it.parent, it.Path, false); err != nil {
			log.Printf("worktree cleanup: keep %s: %v", it.Path, err)
			continue
		}
		if it.Rule == "merged" {
			if out := runCmd("git", it.parent, []string{"branch", "-D", it.Branch}); !out.Success {
				log.Printf("worktree cleanup: branch %s kept: %v", it.Branch, gitErr(out))
			}
		}
		if err := a.DeleteWorkspace(it.ID); err != nil {
			log.Printf("worktree cleanup: row %d: %v", it.ID, err)
		}
		log.Printf("worktree cleanup: removed %s (%s: %s)", it.Path, it.Rule, it.Reason)
		removed = append(removed, it)
	}
	if len(removed) > 0 {
		busEmit("worktrees-cleaned", map[string]any{"removed": removed})
	}
	return removed, nil
}

// WorktreeCleanupPreview lists what both rules would remove right now,
// enabled or not (inactive uses the configured days; 0 leaves it out).
func (a *App) WorktreeCleanupPreview() ([]WorktreeCleanupItem, error) {
	c, err := a.worktreeCleanupSettings()
	if err != nil {
		return nil, err
	}
	items, err := a.worktreeCleanupCandidates(c, c.preview, time.Now(), nil)
	if items == nil {
		items = []WorktreeCleanupItem{}
	}
	return items, err
}

// WorktreeCleanupNow removes the previewed ids the user confirmed. Each one is
// re-evaluated first, so a worktree that gained a chat or an edit since the
// preview stays.
func (a *App) WorktreeCleanupNow(ids []int64) ([]WorktreeCleanupItem, error) {
	c, err := a.worktreeCleanupSettings()
	if err != nil {
		return nil, err
	}
	only := map[int64]bool{}
	for _, id := range ids {
		only[id] = true
	}
	return a.cleanupWorktrees(c, c.preview, time.Now(), only)
}

func (a *App) worktreeCleanupLoop() {
	for range time.Tick(worktreeCleanupInterval) {
		c, err := a.worktreeCleanupSettings()
		if err != nil {
			continue
		}
		if _, err := a.cleanupWorktrees(c, c.auto, time.Now(), nil); err != nil {
			log.Printf("worktree cleanup: %v", err)
		}
	}
}
