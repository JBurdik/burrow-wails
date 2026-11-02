package main

import (
	"path/filepath"
	"sync"
)

// Per-working-tree lock around git mutations that can race on the same tree
// (checkpoint snapshot/restore, worktree add/remove, chat move): a restore
// half-done under a snapshot, or a worktree removed mid-snapshot, loses data.
// Refcounted so the map holds only paths someone is using. Not reentrant —
// never call withWorkspaceLock for a path from inside its own fn.
var workspaceLocks = struct {
	sync.Mutex
	m map[string]*workspaceLock
}{m: map[string]*workspaceLock{}}

type workspaceLock struct {
	sync.Mutex
	users int
}

func withWorkspaceLock(path string, fn func()) {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs // Abs also Cleans
	}
	workspaceLocks.Lock()
	l := workspaceLocks.m[key]
	if l == nil {
		l = &workspaceLock{}
		workspaceLocks.m[key] = l
	}
	l.users++
	workspaceLocks.Unlock()

	defer func() {
		workspaceLocks.Lock()
		if l.users--; l.users == 0 {
			delete(workspaceLocks.m, key)
		}
		workspaceLocks.Unlock()
	}()
	l.Lock()
	defer l.Unlock()
	fn()
}
