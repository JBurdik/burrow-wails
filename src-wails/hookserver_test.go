package main

import (
	"encoding/json"
	"testing"

	"burrow/internal/agentphase"
)

func TestHookEventMapping(t *testing.T) {
	cases := []struct {
		state string
		want  agentphase.Kind
	}{
		{"running", agentphase.HookRunning},
		{"waiting", agentphase.HookWaiting},
		{"permission", agentphase.HookPermission},
		{"done", agentphase.HookDone},
		{"error", agentphase.HookError},
		{"session", agentphase.HookSession},
	}
	for _, c := range cases {
		ev, ok := hookEvent(hookPayload{PtyID: "7", State: c.state})
		if !ok {
			t.Fatalf("%q was dropped", c.state)
		}
		if ev.Kind != c.want {
			t.Fatalf("%q → %q, want %q", c.state, ev.Kind, c.want)
		}
	}
}

func TestHookEventDropsUnknownState(t *testing.T) {
	if _, ok := hookEvent(hookPayload{PtyID: "7", State: "sparkles"}); ok {
		t.Fatal("an unknown hook state must not move the phase")
	}
}

func TestHookEventCarriesDetailAndSessionMetadata(t *testing.T) {
	ev, _ := hookEvent(hookPayload{PtyID: "7", State: "error", Detail: "billing_error"})
	if ev.Detail != "billing_error" {
		t.Fatalf("detail lost: %q", ev.Detail)
	}
	ev, _ = hookEvent(hookPayload{PtyID: "7", State: "session", Model: "opus", Title: "Fix parser"})
	if ev.Model != "opus" || ev.Title != "Fix parser" {
		t.Fatalf("session metadata lost: %+v", ev)
	}
}

// `burrow hook` and the mod both post ptyId as a bare number; a string field
// rejected every one of them with a 400 the CLI never surfaced.
func TestHookPayloadAcceptsNumericAndStringPtyID(t *testing.T) {
	for _, body := range []string{`{"ptyId":37,"state":"done"}`, `{"ptyId":"37","state":"done"}`} {
		var p hookPayload
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if p.PtyID.String() != "37" {
			t.Fatalf("%s: ptyId = %q", body, p.PtyID)
		}
	}
}
