package main

import (
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// PhaseStore owns every agent phase in this environment: PTYs keyed
// "pty:<id>", chats keyed "chat:<id>". One store and one phase type for both,
// because two derivations of the same thing is exactly how the mobile client's
// chat dots drifted from its terminal dots.
type PhaseStore struct {
	mu     sync.Mutex
	db     *sql.DB
	phases map[string]agentphase.Phase
	// seq is a per-id monotonic counter, bumped only on the change path of
	// Apply. Two producers (the hook server and the foreground poll, from
	// their own goroutines) can call Apply for the same id concurrently;
	// nothing else orders which one's persist/busEmit runs last. seq is what
	// lets the later map write win at the DB layer (persist's WHERE guard)
	// and at the bus layer (the staleness check before busEmit below) even
	// if the goroutines are scheduled the other way round. UpdatedAt
	// (millisecond resolution) can't do this job: two Applies inside the
	// same millisecond would tie.
	seq map[string]uint64

	// emitMu serialises everything a client can OBSERVE: the row, the bus
	// event and the terminal_tabs mirror. The seq check alone could not
	// deliver the ordering it documents — goroutine A could pass it, be
	// descheduled, let B persist and emit a newer phase, and then emit the
	// older one last, leaving the UI a step behind a correct DB row with
	// nothing to correct it until the next change.
	//
	// It is deliberately NOT s.mu: sinks must never run under the state lock,
	// or a sink that reads the store back deadlocks.
	emitMu sync.Mutex
}

func NewPhaseStore(db *sql.DB) (*PhaseStore, error) {
	s := &PhaseStore{db: db, phases: make(map[string]agentphase.Phase), seq: make(map[string]uint64)}
	rows, err := db.Query(`SELECT id, state, detail, model, title, is_agent, turn_ended_at, updated_at, seq FROM pty_phase`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p agentphase.Phase
		var seq uint64
		if err := rows.Scan(&id, &p.State, &p.Detail, &p.Model, &p.Title, &p.IsAgent, &p.TurnEndedAt, &p.UpdatedAt, &seq); err != nil {
			return nil, err
		}
		s.phases[id] = p
		s.seq[id] = seq
	}
	return s, rows.Err()
}

// Apply advances one phase. A no-op event writes nothing and emits nothing —
// the pure Next() returning an unchanged value is what makes that cheap.
func (s *PhaseStore) Apply(id string, ev agentphase.Event) {
	s.mu.Lock()
	cur := s.phases[id]
	next := agentphase.Next(cur, ev, time.Now().UnixMilli())
	if next == cur {
		s.mu.Unlock()
		return
	}
	n := s.seq[id] + 1
	s.seq[id] = n
	s.phases[id] = next
	s.mu.Unlock()

	// From here on this Apply is publishing, and publishing is single-file:
	// the staleness check, the write, the emit and the mirror all happen under
	// emitMu, so a concurrent Apply for the same id cannot slip between the
	// check and the emit and leave the older phase as the last one out.
	s.emitMu.Lock()
	defer s.emitMu.Unlock()

	// A concurrent Apply for the same id may have superseded this value
	// between the unlock above and here — or Forget may have dropped the key
	// entirely, in which case seq[id] is gone and can never equal n. Either
	// way this goroutine has nothing left to say: the winner writes and emits
	// its own value, and a forgotten key must not be resurrected.
	s.mu.Lock()
	latest := s.seq[id] == n
	s.mu.Unlock()
	if !latest {
		return
	}

	s.persist(id, next, n)
	busEmit("phase-"+id, next)

	// Mirror for `burrow list-tabs` / MCP list_tabs, which read the DB with no
	// frontend round-trip. The frontend used to make this call itself.
	if ptyID, ok := strings.CutPrefix(id, "pty:"); ok {
		setTabLiveStatus(s.db, ptyID, string(next.State))
	}
}

// persist upserts one phase, guarded by seq so an out-of-order write cannot
// regress the row: the DO UPDATE only fires when the incoming seq is actually
// newer than what's stored. Apply now only reaches here as the latest
// sequence and under emitMu, so this is the second line of defence rather
// than the first — it still stands because the guard is what makes any future
// caller (a batched writer, a replay) safe by construction.
func (s *PhaseStore) persist(id string, p agentphase.Phase, seq uint64) {
	_, err := s.db.Exec(
		`INSERT INTO pty_phase (id, state, detail, model, title, is_agent, turn_ended_at, updated_at, seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state, detail=excluded.detail, model=excluded.model,
		   title=excluded.title, is_agent=excluded.is_agent,
		   turn_ended_at=excluded.turn_ended_at, updated_at=excluded.updated_at,
		   seq=excluded.seq
		 WHERE excluded.seq > pty_phase.seq`,
		id, string(p.State), p.Detail, p.Model, p.Title, p.IsAgent, p.TurnEndedAt, p.UpdatedAt, seq,
	)
	if err != nil {
		log.Printf("persist phase %s: %v", id, err)
	}
}

// Forget drops every trace of one phase key: the row, the map entry and the
// seq counter.
//
// PTY ids are REUSED. The frontend's counter reseeds from
// max(saved, daemon-alive) on restart (src/lib/ptyId.ts), so with tabs 1-3,
// closing 2 and 3 and quitting reseeds to 1 and the next new tab is id 2 —
// which would otherwise adopt pty:2's persisted phase and open wearing a
// green review dot for a turn that ended days ago, under the old session's
// task title.
//
// The seq entry goes with it, and that is safe because Apply's staleness
// check now gates the write as well as the emit: an in-flight Apply that
// computed before the Forget finds seq[id] missing (0) instead of its own n,
// so it cannot resurrect the row it was about to write. A brand-new Apply
// after the Forget starts at seq 1 against a row that no longer exists, so
// the INSERT lands cleanly rather than losing to persist's WHERE guard.
func (s *PhaseStore) Forget(id string) {
	// Same lock as the publish path, so a Forget cannot interleave between a
	// concurrent Apply's staleness check and its write.
	s.emitMu.Lock()
	defer s.emitMu.Unlock()

	s.mu.Lock()
	_, known := s.phases[id]
	delete(s.phases, id)
	delete(s.seq, id)
	s.mu.Unlock()

	if !known {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM pty_phase WHERE id = ?`, id); err != nil {
		log.Printf("forget phase %s: %v", id, err)
	}
}

func (s *PhaseStore) Get(id string) agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.phases[id]
	if !ok {
		// Never seen is IDLE, not "". Phase 4 ships this value over the wire
		// and an empty string would defeat the client's exhaustiveness check.
		return agentphase.Phase{State: agentphase.Idle}
	}
	return p
}

// All is what the snapshot RPC will hand a connecting client (phase 4).
func (s *PhaseStore) All() map[string]agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]agentphase.Phase, len(s.phases))
	for k, v := range s.phases {
		if v.State == "" {
			v.State = agentphase.Idle
		}
		out[k] = v
	}
	return out
}

// Replay re-emits a known phase after a view attaches. A terminal thread can
// already be running before its XTerm mounts; without this its first hook is
// lost and the dot stays idle until the next one arrives.
func (s *PhaseStore) Replay(id string) {
	s.mu.Lock()
	p, ok := s.phases[id]
	s.mu.Unlock()
	if ok {
		busEmit("phase-"+id, p)
	}
}
