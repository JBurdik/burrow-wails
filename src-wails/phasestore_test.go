package main

import (
	"sync"
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

// TestPhaseStoreConcurrentApplyStaysOrdered drives the seq machinery the way
// production does — two producers (the hook server and the foreground poll)
// applying to the SAME id from their own goroutines — instead of through a
// synthetic persist() call. Run under -race.
//
// Two properties matter, and they are the two the emitMu section exists for:
// the LAST phase a client saw must be the one the store and the DB settled on
// (otherwise the UI is a step behind a correct row, with nothing to correct
// it), and emitted phases must arrive in seq order, which UpdatedAt witnesses
// because it is sampled inside the same critical section that assigns seq.
func TestPhaseStoreConcurrentApplyStaysOrdered(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	var mu sync.Mutex
	var seen []agentphase.Phase
	busSubscribe(func(name string, payload any) {
		if name != "phase-pty:7" {
			return
		}
		p, ok := payload.(agentphase.Phase)
		if !ok {
			t.Errorf("bus payload is not a Phase: %T", payload)
			return
		}
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})

	s, _ := newTestStore(t)

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(2)
	// The hook producer: whole turns.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
			s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookWaiting})
			s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookDone})
		}
	}()
	// The poll producer: the agent flag, flipping under the hooks.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollAgent, Bool: i%2 == 0})
			s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollNotBusy})
		}
	}()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("nothing was emitted")
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].UpdatedAt < seen[i-1].UpdatedAt {
			t.Fatalf("emit %d went backwards in time: %+v after %+v", i, seen[i], seen[i-1])
		}
	}

	last := seen[len(seen)-1]
	if got := s.Get("pty:7"); got != last {
		t.Fatalf("the last emitted phase is not the stored one:\n emit  %+v\n store %+v", last, got)
	}

	var row agentphase.Phase
	err := s.db.QueryRow(
		`SELECT state, detail, model, title, is_agent, turn_ended_at, updated_at FROM pty_phase WHERE id = ?`,
		"pty:7",
	).Scan(&row.State, &row.Detail, &row.Model, &row.Title, &row.IsAgent, &row.TurnEndedAt, &row.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if row != last {
		t.Fatalf("the DB and the last emit disagree:\n emit %+v\n row  %+v", last, row)
	}
}

// TestPhaseStoreForgetStartsClean covers the reused-pty-id bug: ids come from a
// counter that reseeds from max(saved, daemon-alive), so a brand-new tab can be
// handed the id of a tab that finished a turn days ago. A forgotten id must
// start from idle in memory, on disk and after a restart.
func TestPhaseStoreForgetStartsClean(t *testing.T) {
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
	s.Apply("pty:2", agentphase.Event{Kind: agentphase.HookSession, Title: "refactor the parser"})
	s.Apply("pty:2", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:2", agentphase.Event{Kind: agentphase.HookDone})

	s.Forget("pty:2")

	got := s.Get("pty:2")
	if got.State != agentphase.Idle {
		// Never-seen is idle, not "": phase 4 ships this over the wire.
		t.Fatalf("a forgotten id is not idle: %+v", got)
	}
	if got.TurnEndedAt != 0 || got.Title != "" || got.IsAgent {
		t.Fatalf("a forgotten id kept the old session: %+v", got)
	}
	if _, ok := s.All()["pty:2"]; ok {
		t.Fatal("a forgotten id is still in the snapshot the poll and phase 4 read")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pty_phase WHERE id = ?`, "pty:2").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the row survived Forget")
	}

	// The reused id starts a fresh sequence against a deleted row — the INSERT
	// must land rather than lose to persist's WHERE guard.
	s.Apply("pty:2", agentphase.Event{Kind: agentphase.HookRunning})
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
	after := s2.Get("pty:2")
	if after.State != agentphase.Running {
		t.Fatalf("the reused id did not persist its own phase: %+v", after)
	}
	if after.Title != "" || after.TurnEndedAt != 0 {
		t.Fatalf("the forgotten session leaked back across a restart: %+v", after)
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
