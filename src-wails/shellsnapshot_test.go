package main

import (
	"encoding/json"
	"strings"
	"testing"

	"burrow/internal/agentphase"
)

func TestSnapshotCarriesSeqTakenBeforeData(t *testing.T) {
	// Read the other way round — data first, seq last — an event landing
	// between the two reads carries a number BELOW the snapshot's seq, so the
	// client counts it as already seen and never resumes it. Lost for good.
	// The hook lands exactly one event in that window; asserting on the
	// snapshot's own seq is what fails if the two reads ever swap.
	shellStreamReset()
	recordShellEvent("before", nil)

	shellSnapshotAfterSeqHook = func() { recordShellEvent("during", nil) }
	t.Cleanup(func() { shellSnapshotAfterSeqHook = nil })

	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := currentSeq(); got != 2 {
		t.Fatalf("the hook did not land its event: current seq %d, want 2", got)
	}
	if snap.Seq != 1 {
		t.Fatalf("snapshot seq %d, want 1 — an event landing mid-read must be left for resume", snap.Seq)
	}
}

func TestSnapshotCollectionsMarshalAsEmptyNotNull(t *testing.T) {
	// A client does snapshot.workspaces.map(...) and snapshot.chats.map(...)
	// with no nil guard. JSON null in either kills the first paint over a
	// missing chat list or an empty database, which is the one state every
	// fresh install is in.
	shellStreamReset()
	app := &App{}
	snap, err := app.ShellSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"workspaces":[]`, `"tabs":{}`, `"phases":{}`, `"chats":[]`} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("missing %s in %s", want, blob)
		}
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
