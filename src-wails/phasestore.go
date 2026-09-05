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
}

func NewPhaseStore(db *sql.DB) (*PhaseStore, error) {
	s := &PhaseStore{db: db, phases: make(map[string]agentphase.Phase)}
	rows, err := db.Query(`SELECT id, state, detail, model, title, is_agent, turn_ended_at, updated_at FROM pty_phase`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p agentphase.Phase
		if err := rows.Scan(&id, &p.State, &p.Detail, &p.Model, &p.Title, &p.IsAgent, &p.TurnEndedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		s.phases[id] = p
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
	s.phases[id] = next
	s.mu.Unlock()

	s.persist(id, next)
	busEmit("phase-"+id, next)

	// Mirror for `burrow list-tabs` / MCP list_tabs, which read the DB with no
	// frontend round-trip. The frontend used to make this call itself.
	if ptyID, ok := strings.CutPrefix(id, "pty:"); ok {
		setTabLiveStatus(s.db, ptyID, string(next.State))
	}
}

func (s *PhaseStore) persist(id string, p agentphase.Phase) {
	_, err := s.db.Exec(
		`INSERT INTO pty_phase (id, state, detail, model, title, is_agent, turn_ended_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state, detail=excluded.detail, model=excluded.model,
		   title=excluded.title, is_agent=excluded.is_agent,
		   turn_ended_at=excluded.turn_ended_at, updated_at=excluded.updated_at`,
		id, string(p.State), p.Detail, p.Model, p.Title, p.IsAgent, p.TurnEndedAt, p.UpdatedAt,
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
