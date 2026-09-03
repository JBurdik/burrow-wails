package agentproc

import (
	"testing"
	"time"
)

func TestRemoveIfCurrentKeepsReplacementSession(t *testing.T) {
	m := NewManager()
	old := &Session{ID: "chat-1"}
	replacement := &Session{ID: "chat-1"}
	m.sessions[old.ID] = replacement

	m.removeIfCurrent(old.ID, old)

	if got := m.sessions[old.ID]; got != replacement {
		t.Fatalf("old process removed replacement session: got %#v, want %#v", got, replacement)
	}
}

// The shutdown leak: agent CLIs outliving the app. StopAll must actually reap
// the processes and empty the registry.
func TestStopAllKillsEveryLiveSession(t *testing.T) {
	m := NewManager()
	for _, id := range []string{"chat-1", "chat-2", "chat-3"} {
		if err := m.Start(id, "sleep", []string{"60"}, "", nil, func(string) {}, func() {}); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	m.StopAll()

	if len(m.sessions) != 0 {
		t.Fatalf("sessions still registered after StopAll: %v", m.sessions)
	}
	for _, id := range []string{"chat-1", "chat-2", "chat-3"} {
		if m.Alive(id) {
			t.Fatalf("%s still alive after StopAll", id)
		}
	}
}

func TestReapIdleStopsOnlySilentSessions(t *testing.T) {
	m := NewManager()
	defer m.StopAll()
	for _, id := range []string{"stale", "fresh"} {
		if err := m.Start(id, "sleep", []string{"60"}, "", nil, func(string) {}, func() {}); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}
	// Backdate one session past the threshold; the other stays recent.
	m.mu.Lock()
	m.sessions["stale"].lastSeen = time.Now().Add(-time.Hour)
	m.mu.Unlock()

	reaped := m.ReapIdle(30 * time.Minute)

	if len(reaped) != 1 || reaped[0] != "stale" {
		t.Fatalf("reaped = %v, want [stale]", reaped)
	}
	if m.Alive("stale") {
		t.Fatal("stale session survived the sweep")
	}
	if !m.Alive("fresh") {
		t.Fatal("fresh session was reaped")
	}
}

// A streaming turn writes no stdin, so output alone has to keep it alive.
func TestOutputKeepsSessionFromBeingReaped(t *testing.T) {
	m := NewManager()
	defer m.StopAll()
	lines := make(chan string, 1)
	if err := m.Start("busy", "echo", []string{"hello"}, "", nil,
		func(l string) { lines <- l }, func() {}); err != nil {
		t.Fatalf("start: %v", err)
	}
	m.mu.Lock()
	m.sessions["busy"].lastSeen = time.Now().Add(-time.Hour)
	m.mu.Unlock()

	<-lines // the streamed line must have touched lastSeen

	if reaped := m.ReapIdle(30 * time.Minute); len(reaped) != 0 {
		t.Fatalf("reaped a session that just streamed output: %v", reaped)
	}
}
