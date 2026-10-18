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

// Send now = Stop + Start under the same id + send. The killed process exits
// asynchronously; its exit must not reach the channel the replacement now owns.
func TestStoppedSessionIsSilenced(t *testing.T) {
	m := NewManager()
	exited := make(chan struct{}, 1)
	if err := m.Start("chat-1", "sleep", []string{"60"}, "", nil, func(string) {}, func() { exited <- struct{}{} }); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := m.Stop("chat-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-exited:
		t.Fatal("onExit fired for an explicitly stopped session")
	case <-time.After(500 * time.Millisecond):
	}
}
