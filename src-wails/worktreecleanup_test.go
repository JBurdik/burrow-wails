package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorktreeCleanupVerdict(t *testing.T) {
	now := time.Unix(100*24*3600, 0)
	day := 24 * time.Hour
	both := worktreeCleanupRules{Merged: true, InactiveDays: 14}
	// idle: old, clean, nothing open — the inactive rule removes it.
	idle := worktreeFacts{Created: now.Add(-30 * day), LastActivity: now.Add(-20 * day)}
	merged := idle
	merged.Merged, merged.Chats, merged.Tabs, merged.LastActivity = true, 2, 1, now.Add(-2*time.Hour)
	with := func(f worktreeFacts, mut func(*worktreeFacts)) worktreeFacts { mut(&f); return f }

	cases := []struct {
		name string
		f    worktreeFacts
		r    worktreeCleanupRules
		want string
	}{
		{"inactive past N days", idle, both, "inactive"},
		{"inactive exactly N days", with(idle, func(f *worktreeFacts) { f.LastActivity = now.Add(-14 * day) }), both, "inactive"},
		{"inactive under N days", with(idle, func(f *worktreeFacts) { f.LastActivity = now.Add(-13 * day) }), both, ""},
		{"inactive rule off (0 days)", idle, worktreeCleanupRules{Merged: true}, ""},
		{"inactive but has a chat", with(idle, func(f *worktreeFacts) { f.Chats = 1 }), both, ""},
		{"inactive but has a tab row", with(idle, func(f *worktreeFacts) { f.Tabs = 1 }), both, ""},
		{"merged with chats and tabs", merged, both, "merged"},
		{"merged rule off", merged, worktreeCleanupRules{InactiveDays: 14}, ""},
		{"no rules", idle, worktreeCleanupRules{}, ""},

		// Every protection beats both rules.
		{"protect: dirty", with(merged, func(f *worktreeFacts) { f.Dirty = true }), both, ""},
		{"protect: dirty (inactive)", with(idle, func(f *worktreeFacts) { f.Dirty = true }), both, ""},
		{"protect: live chat process", with(merged, func(f *worktreeFacts) { f.LiveProcess = true }), both, ""},
		{"protect: open pty", with(merged, func(f *worktreeFacts) { f.OpenPty = true }), both, ""},
		{"protect: younger than 1h", with(merged, func(f *worktreeFacts) { f.Created = now.Add(-59 * time.Minute) }), both, ""},
		{"protect: younger than 1h (inactive)", with(idle, func(f *worktreeFacts) {
			f.Created, f.LastActivity = now.Add(-time.Minute), now.Add(-20*day)
		}), both, ""},
		{"grace over at 1h", with(merged, func(f *worktreeFacts) { f.Created = now.Add(-time.Hour) }), both, "merged"},
		{"protect: active workspace", with(merged, func(f *worktreeFacts) { f.Active = true }), both, ""},
		{"protect: pinned chat", with(merged, func(f *worktreeFacts) { f.Pinned = true }), both, ""},
	}
	for _, c := range cases {
		if got, _ := worktreeCleanupVerdict(c.f, c.r, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// End to end over a real repo: the inactive rule removes an idle worktree's
// directory and row and keeps its branch; a dirty one stays.
func TestCleanupWorktreesInactive(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("HOME", t.TempDir()) // ReadConfig → an absent config.json
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	git := func(cwd string, args ...string) {
		t.Helper()
		if out := runCmd("git", cwd, args); !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	os.MkdirAll(repo, 0o755)
	git(repo, "init", "-q", "-b", "main")
	git(repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	parent, err := a.CreateWorkspace("repo", repo)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	mk := func(name string) (int64, string) {
		p := filepath.Join(dir, name)
		git(repo, "worktree", "add", "-q", "-b", name, p)
		w, err := a.CreateWorkspace(name, p)
		if err != nil {
			t.Fatal(err)
		}
		a.db.Exec(`UPDATE workspaces SET parent_id = ?, worktree_branch = ?, created_at = ? WHERE id = ?`, parent.ID, name, old, w.ID)
		return w.ID, p
	}
	idleID, idlePath := mk("idle")
	dirtyID, dirtyPath := mk("dirty")
	os.WriteFile(filepath.Join(dirtyPath, "new.txt"), []byte("x"), 0o644) // untracked only

	c, err := a.worktreeCleanupSettings()
	if err != nil {
		t.Fatal(err)
	}
	if c.auto.Merged || c.auto.InactiveDays != 0 || c.preview.InactiveDays != 14 {
		t.Fatalf("defaults: auto %+v preview %+v", c.auto, c.preview)
	}
	removed, err := a.cleanupWorktrees(c, c.preview, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].ID != idleID || removed[0].Rule != "inactive" {
		t.Fatalf("removed %+v", removed)
	}
	if _, err := os.Stat(idlePath); !os.IsNotExist(err) {
		t.Errorf("idle worktree dir still there: %v", err)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id IN (?, ?)`, idleID, dirtyID).Scan(&n)
	if n != 1 {
		t.Errorf("workspace rows left = %d, want 1 (the dirty one)", n)
	}
	if !runCmd("git", repo, []string{"show-ref", "--verify", "--quiet", "refs/heads/idle"}).Success {
		t.Error("inactive rule deleted the branch")
	}
}
