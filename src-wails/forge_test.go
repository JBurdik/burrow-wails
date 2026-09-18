package main

import "testing"

// A worktree inherits its root repo's provider override: the override is set on
// the repo the user configured, and every worktree of it is the same forge.
func TestForgeProviderClimbsToRootRepo(t *testing.T) {
	a := newTestApp(t)
	repo, err := a.CreateWorkspace("repo", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	wt, err := a.CreateWorkspace("wt", "/tmp/repo-wt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE workspaces SET parent_id = ? WHERE id = ?`, repo.ID, wt.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetForgeProvider(repo.ID, "gitlab"); err != nil {
		t.Fatal(err)
	}
	if got := a.providerOverride("/tmp/repo-wt"); got != "gitlab" {
		t.Errorf("worktree override = %q, want gitlab", got)
	}
	if got := a.providerOverride("/tmp/repo"); got != "gitlab" {
		t.Errorf("repo override = %q, want gitlab", got)
	}
	if got := a.providerOverride("/tmp/unknown"); got != "" {
		t.Errorf("unknown path override = %q, want empty", got)
	}
}

func TestSetForgeProviderRejectsUnknown(t *testing.T) {
	a := newTestApp(t)
	ws, err := a.CreateWorkspace("repo", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetForgeProvider(ws.ID, "bitbucket"); err == nil {
		t.Fatal("expected an error for a provider with no adapter")
	}
	// Clearing is legal: it hands detection back to the remote URL.
	if err := a.SetForgeProvider(ws.ID, ""); err != nil {
		t.Fatal(err)
	}
}
