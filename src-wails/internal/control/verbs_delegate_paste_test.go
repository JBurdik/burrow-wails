package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type pastePTY struct{ writes []string }

func (p *pastePTY) WritePty(_ string, text string) error {
	p.writes = append(p.writes, text)
	return nil
}

type pasteUI struct {
	pty   *pastePTY
	reads int
}

func (u *pasteUI) Do(_ context.Context, action string, _ map[string]any) (json.RawMessage, error) {
	u.reads++
	if action != "tab_output" {
		panic(action)
	}
	text := "Claude prompt ready"
	if len(u.pty.writes) > 0 {
		text = "Claude prompt ready\nfirst line of draft"
	}
	return json.Marshal(map[string]any{"text": text})
}

func TestSendToTabPastesThenSubmitsAfterDraft(t *testing.T) {
	pty := &pastePTY{}
	ui := &pasteUI{pty: pty}
	c := newTestCore(t, Deps{PTY: pty, UI: ui, Phases: &fakePhases{state: "idle"}})
	_, err := c.Call(context.Background(), ScopeLocal, "send_to_tab", Params{"pty_id": float64(42), "text": "first line\nsecond line"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pty.writes) != 2 || pty.writes[0] != pasteStart+"first line\nsecond line"+pasteEnd || pty.writes[1] != "\r" {
		t.Fatalf("writes = %#v", pty.writes)
	}
	if ui.reads < 6 {
		t.Fatalf("readiness captures = %d, want at least 6", ui.reads)
	}
}

func TestSendToTabDoesNotSubmitWithoutDraft(t *testing.T) {
	pty := &pastePTY{}
	// This UI never changes after the paste, so the submit must time out.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := newTestCore(t, Deps{PTY: pty, UI: &fixedPasteUI{}})
	_, err := c.Call(ctx, ScopeLocal, "send_to_tab", Params{"pty_id": float64(42), "text": "hello"})
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("error = %v", err)
	}
	if len(pty.writes) != 1 || pty.writes[0] != pasteStart+"hello"+pasteEnd {
		t.Fatalf("writes = %#v", pty.writes)
	}
}

type fixedPasteUI struct{}

func (*fixedPasteUI) Do(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"text": "unchanged prompt"})
}

func TestSendToTabLeavesDraftWithoutSubmit(t *testing.T) {
	pty := &pastePTY{}
	c := newTestCore(t, Deps{PTY: pty, UI: &fixedPasteUI{}})
	_, err := c.Call(context.Background(), ScopeLocal, "send_to_tab", Params{"pty_id": float64(42), "text": "hello", "submit": false})
	if err != nil {
		t.Fatal(err)
	}
	if len(pty.writes) != 1 || pty.writes[0] != pasteStart+"hello"+pasteEnd {
		t.Fatalf("writes = %#v", pty.writes)
	}
}

func TestSendToTabWaitsForAgentPhase(t *testing.T) {
	pty := &pastePTY{}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	c := newTestCore(t, Deps{PTY: pty, UI: &fixedPasteUI{}, Phases: &fakePhases{state: "running"}})
	_, err := c.Call(ctx, ScopeLocal, "send_to_tab", Params{"pty_id": float64(42), "text": "hello"})
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("error = %v", err)
	}
	if len(pty.writes) != 0 {
		t.Fatalf("writes while agent is running = %#v", pty.writes)
	}
}
