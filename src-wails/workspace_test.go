package main

import "testing"

func newWorkspaceApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &App{db: db}
}

func TestWorkspaceMutationsEmitWorkspacesChanged(t *testing.T) {
	// Every mutation of the LIST notifies; a client that is already connected
	// has no other way to learn (shell_snapshot only runs on connect).
	// Exactly once, not twice: a doubled emit is a doubled reload on the phone.
	a := newWorkspaceApp(t)
	t.Cleanup(busReset)
	busReset()

	var events []string
	busSubscribe(func(ev shellEvent) {
		if ev.Name == "workspaces-changed" {
			events = append(events, ev.Name)
		}
	})

	ws, err := a.CreateWorkspace("test", "/tmp/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("CreateWorkspace: want 1 emit, got %d", len(events))
	}

	events = nil
	if err := a.RenameWorkspace(ws.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("RenameWorkspace: want 1 emit, got %d", len(events))
	}

	events = nil
	if err := a.SetWorkspaceIcon(ws.ID, "rocket"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("SetWorkspaceIcon: want 1 emit, got %d", len(events))
	}

	events = nil
	if err := a.SetWorkspaceOrder([]int64{ws.ID}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("SetWorkspaceOrder: want 1 emit, got %d", len(events))
	}

	events = nil
	if err := a.DeleteWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("DeleteWorkspace: want 1 emit, got %d", len(events))
	}
}

func TestTouchWorkspaceDoesNotEmit(t *testing.T) {
	// last_opened is not a change to the list, and it fires on every
	// workspace switch — emitting there would flood both clients.
	a := newWorkspaceApp(t)
	t.Cleanup(busReset)
	busReset()

	ws, err := a.CreateWorkspace("test", "/tmp/test")
	if err != nil {
		t.Fatal(err)
	}

	var events []string
	busSubscribe(func(ev shellEvent) {
		if ev.Name == "workspaces-changed" {
			events = append(events, ev.Name)
		}
	})

	if err := a.TouchWorkspace(ws.ID); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("TouchWorkspace: want 0 emits, got %d", len(events))
	}
}

func TestAFailedMutationDoesNotEmit(t *testing.T) {
	// e.g. rename of an id that does not exist. The event claims the list
	// changed; if it did not, every client does a pointless round trip and
	// learns nothing.
	a := newWorkspaceApp(t)
	t.Cleanup(busReset)
	busReset()

	var events []string
	busSubscribe(func(ev shellEvent) {
		if ev.Name == "workspaces-changed" {
			events = append(events, ev.Name)
		}
	})

	// A rename of an id that does not exist returns no SQL error (an UPDATE
	// matching zero rows is not an error) but changed nothing — RowsAffected
	// is what tells the two apart, and only a real change should emit.
	if err := a.RenameWorkspace(9999, "nope"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("no-op RenameWorkspace: want 0 emits, got %d", len(events))
	}
}
