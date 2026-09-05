package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func newTestStore(t *testing.T) (*PhaseStore, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewPhaseStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestPhaseStoreEmitsOnChange(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	var events []string
	busSubscribe(func(name string, _ any) { events = append(events, name) })

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	if len(events) != 1 || events[0] != "phase-pty:7" {
		t.Fatalf("bad emit: %v", events)
	}
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("state not stored: %+v", s.Get("pty:7"))
	}
}

func TestPhaseStoreSilentOnNoop(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	var count int
	busSubscribe(func(string, any) { count++ })

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	if count != 1 {
		t.Fatalf("a repeated event emitted %d times", count)
	}
}

func TestPhaseStoreSurvivesRestart(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPhaseStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookError, Detail: "rate_limit"})
	db.Close()

	db2, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := NewPhaseStore(db2)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get("pty:7")
	if got.State != agentphase.Failed || got.Detail != "rate_limit" {
		t.Fatalf("phase did not survive restart: %+v", got)
	}
	if got.TurnEndedAt == 0 {
		t.Fatal("turn end lost across restart — the read receipt needs it")
	}
}

// TestPhaseStorePersistGuardsAgainstStaleSeq covers the race the review found:
// two goroutines can call Apply for the same id concurrently (the hook server
// and the foreground poll do, in later tasks), and nothing else orders which
// one's persist() lands last. Here we simulate the loser — a goroutine that
// computed the *earlier* phase ("running") but only reaches persist after a
// newer one ("done") already landed — by calling persist directly with a
// stale seq. Without the seq guard in persist's WHERE clause, this overwrites
// the row and the regression survives a restart.
func TestPhaseStorePersistGuardsAgainstStaleSeq(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewPhaseStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning}) // seq 1
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookDone})    // seq 2

	stale := s.Get("pty:7")
	stale.State = agentphase.Running
	stale.TurnEndedAt = 0
	s.persist("pty:7", stale, 1) // late write, seq 1 — must lose to seq 2

	var state string
	if err := db.QueryRow(`SELECT state FROM pty_phase WHERE id = ?`, "pty:7").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(agentphase.Done) {
		t.Fatalf("stale write won: state=%s", state)
	}
	db.Close()

	db2, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := NewPhaseStore(db2)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("pty:7").State; got != agentphase.Done {
		t.Fatalf("phase regressed across restart: %v", got)
	}
}

func TestPhaseStoreReplayReemits(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	var replayed int
	busSubscribe(func(name string, _ any) {
		if name == "phase-pty:7" {
			replayed++
		}
	})
	s.Replay("pty:7")
	if replayed != 1 {
		t.Fatalf("replay emitted %d times", replayed)
	}
	s.Replay("pty:999")
	if replayed != 1 {
		t.Fatal("replay of an unknown id emitted something")
	}
}
