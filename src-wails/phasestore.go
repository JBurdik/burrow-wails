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

	s.persist(id, next, n)

	// A concurrent Apply for the same id may have already superseded this
	// value between the unlock above and here. persist's own WHERE guard
	// already stopped it from winning the DB row; this stops it from also
	// going out over the bus (and into the terminal_tabs mirror below) a
	// step behind what was already emitted.
	s.mu.Lock()
	latest := s.seq[id] == n
	s.mu.Unlock()
	if !latest {
		return
	}

	busEmit("phase-"+id, next)

	// Mirror for `burrow list-tabs` / MCP list_tabs, which read the DB with no
	// frontend round-trip. The frontend used to make this call itself.
	if ptyID, ok := strings.CutPrefix(id, "pty:"); ok {
		setTabLiveStatus(s.db, ptyID, string(next.State))
	}
}

// persist upserts one phase, guarded by seq so an out-of-order write (an
// older Apply's goroutine reaching this call after a newer one already has)
// cannot regress the row: the DO UPDATE only fires when the incoming seq is
// actually newer than what's stored.
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

func (s *PhaseStore) Get(id string) agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phases[id]
}

// All is what the snapshot RPC will hand a connecting client (phase 4).
func (s *PhaseStore) All() map[string]agentphase.Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]agentphase.Phase, len(s.phases))
	for k, v := range s.phases {
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
