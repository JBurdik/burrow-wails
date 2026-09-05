package main

import (
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
