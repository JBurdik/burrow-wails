package agentproc

import "testing"

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
