package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Same path (spelled two ways) serialises; a different path doesn't wait; the
// map is empty once nobody holds a lock.
func TestWorkspaceLock(t *testing.T) {
	dir := t.TempDir()
	var inside int32
	held, release := make(chan struct{}), make(chan struct{})
	go withWorkspaceLock(dir, func() {
		atomic.StoreInt32(&inside, 1)
		close(held)
		<-release
		atomic.StoreInt32(&inside, 0)
	})
	<-held

	other := make(chan struct{})
	go withWorkspaceLock(filepath.Join(dir, "elsewhere"), func() { close(other) })
	select {
	case <-other:
	case <-time.After(2 * time.Second):
		t.Fatal("a different path blocked on the held lock")
	}

	same := make(chan int32)
	go withWorkspaceLock(dir+"/sub/..", func() { same <- atomic.LoadInt32(&inside) })
	select {
	case <-same:
		t.Fatal("same path ran while the lock was held")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if got := <-same; got != 0 {
		t.Fatal("same path overlapped the holder")
	}

	workspaceLocks.Lock()
	n := len(workspaceLocks.m)
	workspaceLocks.Unlock()
	if n != 0 {
		t.Fatalf("lock map leaked %d entries", n)
	}
}

// git's "contains modified or untracked files" check honours
// status.showUntrackedFiles=no; a non-forced remove must still refuse.
func TestRemoveWorktreeKeepsUntrackedFiles(t *testing.T) {
	repo := t.TempDir()
	git := func(cwd string, args ...string) {
		t.Helper()
		if out := runCmd("git", cwd, args); !out.Success {
			t.Fatalf("git %v: %s", args, out.Stderr)
		}
	}
	git(repo, "init", "-q")
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	git(repo, "config", "status.showUntrackedFiles", "no")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", ".")
	git(repo, "commit", "-q", "-m", "c")

	wt := filepath.Join(t.TempDir(), "wt")
	git(repo, "worktree", "add", "-q", wt, "-b", "side")
	untracked := filepath.Join(wt, "precious.txt")
	if err := os.WriteFile(untracked, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeWorktree(repo, wt, false); err == nil {
		t.Fatal("non-forced remove of a worktree with untracked files succeeded")
	}
	if _, err := os.Stat(untracked); err != nil {
		t.Fatalf("untracked file gone: %v", err)
	}
	if err := removeWorktree(repo, wt, true); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
}
