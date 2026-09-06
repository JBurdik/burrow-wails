package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func TestSnapshotCarriesSeqTakenBeforeData(t *testing.T) {
	shellStreamReset()
	recordShellEvent("a", nil)

	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != 1 {
		t.Fatalf("snapshot seq %d, want the current 1", snap.Seq)
	}
}

func TestSnapshotCarriesPhases(t *testing.T) {
	shellStreamReset()
	t.Cleanup(busReset)
	busReset()

	store, _ := newTestStore(t)
	store.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	app := &App{phases: store}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := snap.Phases["pty:7"]; !ok || got.State != agentphase.Running {
		t.Fatalf("phase missing or wrong: %+v", snap.Phases)
	}
}

func TestSnapshotToleratesAMissingStore(t *testing.T) {
	// The DB can fail to open; a snapshot must degrade rather than panic,
	// because the client's first paint depends on it.
	shellStreamReset()
	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatalf("a snapshot with no store must not error: %v", err)
	}
	if snap.Phases == nil || snap.Tabs == nil {
		t.Fatal("empty maps, not nil, so the client can index them")
	}
}
