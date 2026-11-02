package main

import (
	"testing"
	"time"

	ap "burrow/internal/agentphase"
)

func TestPushReason(t *testing.T) {
	for _, c := range []struct {
		key        string
		prev, next ap.State
		want       string
	}{
		{"chat:1", ap.Running, ap.WaitingApproval, "permission"},
		{"pty:7", ap.Running, ap.WaitingApproval, "permission"},
		{"chat:1", ap.Running, ap.WaitingInput, "waiting"},
		{"pty:7", ap.Running, ap.WaitingInput, "waiting"},
		{"chat:1", ap.Running, ap.Done, "done"},
		{"pty:7", ap.Running, ap.Done, ""}, // a tab's turn end is not pushed
		{"chat:1", ap.WaitingApproval, ap.WaitingApproval, ""},
		{"chat:1", ap.Idle, ap.Running, ""},
		{"chat:1", ap.Running, ap.Failed, ""},
		{"chat:1", ap.Done, ap.Stale, ""},
	} {
		if got := pushReason(c.key, c.prev, c.next); got != c.want {
			t.Errorf("%s %s→%s = %q, want %q", c.key, c.prev, c.next, got, c.want)
		}
	}
}

func TestPushGate(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	var never time.Time
	for _, c := range []struct {
		name            string
		watched, pushed time.Time
		want            bool
	}{
		{"fresh", never, never, true},
		{"watched 30s ago", now.Add(-30 * time.Second), never, false},
		{"watched 89s ago", now.Add(-89 * time.Second), never, false},
		{"watched 2 min ago", now.Add(-2 * time.Minute), never, true},
		{"pushed 10s ago", never, now.Add(-10 * time.Second), false},
		{"pushed 30s ago", never, now.Add(-30 * time.Second), true},
	} {
		if got := pushGate(c.watched, c.pushed, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	// claim stamps: the second push inside the window is refused.
	p := newWebPusher("")
	if !p.claim("chat:1", now) || p.claim("chat:1", now.Add(5*time.Second)) {
		t.Fatal("rate limit not applied by claim")
	}
	if !p.claim("chat:2", now) {
		t.Fatal("rate limit leaked across keys")
	}
	p.watch("chat:3", now)
	if p.claim("chat:3", now.Add(time.Second)) {
		t.Fatal("a watched chat was pushed")
	}
}
