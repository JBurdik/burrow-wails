package main

import (
	"testing"

	"burrow/internal/agentphase"
)

// The cases of src/stores/claudeChats.test.ts "settledFor", ported: the client
// rule is now `!inFlight && settled_since > 0 && now >= settled_since`. The
// in-flight cases (running / waiting / permission) are the client's gate and
// are covered by src/lib/threadAttention.test.ts.
func TestChatAttentionSettled(t *testing.T) {
	const day = int64(24 * 60 * 60 * 1000)
	const now = int64(1_800_000_000_000)
	settled := func(c Chat) bool {
		s := chatAttentionFor(c, now).SettledSince
		return s > 0 && now >= s
	}
	cases := []struct {
		name string
		c    Chat
		want bool
	}{
		{"fresh empty chat stays active", Chat{LastActivityAt: now - 1000}, false},
		{"untouched 3 days settles", Chat{LastActivityAt: now - 3*day}, true},
		{"1 ms short of 3 days stays", Chat{LastActivityAt: now - (3*day - 1)}, false},
		{"no timestamp settles", Chat{LastActivityAt: 0}, true},
		{"manual settle beats the clock", Chat{LastActivityAt: now, SettledOverride: "settled"}, true},
		{"manual active beats the clock", Chat{LastActivityAt: 0, SettledOverride: "active"}, false},
		{"snoozed is settled", Chat{LastActivityAt: now, SnoozedUntil: now + 1}, true},
		{"snooze beats manual active", Chat{LastActivityAt: now, SnoozedUntil: now + 1, SettledOverride: "active"}, true},
		{"expired snooze falls through", Chat{LastActivityAt: now, SnoozedUntil: now}, false},
		{"needs you is never auto-settled", Chat{LastActivityAt: 0, TurnNeeds: `{"needs":"you"}`}, false},
		{"needs you still yields to a snooze", Chat{TurnNeeds: `{"needs":"you"}`, SnoozedUntil: now + 1}, true},
		{"needs you still yields to manual settle", Chat{TurnNeeds: `{"needs":"you"}`, SettledOverride: "settled"}, true},
		{"needs nothing ages like any other", Chat{LastActivityAt: 0, TurnNeeds: `{"needs":"nothing"}`}, true},
	}
	for _, tc := range cases {
		if got := settled(tc.c); got != tc.want {
			t.Errorf("%s: settled = %v, want %v", tc.name, got, tc.want)
		}
	}
	// The 3-day flip is a timestamp the client compares, not a bool Go must re-emit.
	if s := chatAttentionFor(Chat{LastActivityAt: now}, now).SettledSince; s != now+3*day {
		t.Errorf("settled_since = %d, want %d", s, now+3*day)
	}
}

func TestChatAttentionTurnNeeds(t *testing.T) {
	for _, tc := range []struct {
		raw             string
		needs, saidDone bool
	}{
		{"", false, false},
		{`{"needs":"you","asks":[{"question":"q"}]}`, true, false},
		{`{"needs":"nothing","next":["x"]}`, false, true},
		{`not json`, false, false},
	} {
		a := chatAttentionFor(Chat{TurnNeeds: tc.raw}, 0)
		if a.NeedsYou != tc.needs || a.SaidDone != tc.saidDone {
			t.Errorf("%q: got needs=%v saidDone=%v", tc.raw, a.NeedsYou, a.SaidDone)
		}
	}
}

func TestSnapshotKeepsPhase(t *testing.T) {
	live := map[string]bool{"7": true}
	chats := map[int64]bool{3: true}
	for _, tc := range []struct {
		key  string
		ptys map[string]bool
		want bool
	}{
		{"pty:7", live, true},
		{"pty:8", live, false},
		{"pty:8", nil, true}, // daemon unreadable: keep
		{"chat:3", live, true},
		{"chat:4", live, false}, // archived or deleted
		{"138", live, false},
	} {
		if got := snapshotKeepsPhase(tc.key, tc.ptys, chats); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestPrunablePtyPhase(t *testing.T) {
	const now, week = int64(1_800_000_000_000), int64(7 * 24 * 60 * 60 * 1000)
	for _, tc := range []struct {
		name  string
		ph    agentphase.Phase
		alive bool
		want  bool
	}{
		{"live pty kept", agentphase.Phase{State: agentphase.Idle}, true, false},
		{"dead idle pruned", agentphase.Phase{State: agentphase.Idle}, false, true},
		{"dead stale kept a while", agentphase.Phase{State: agentphase.Stale, UpdatedAt: now}, false, false},
		{"dead stale a week old pruned", agentphase.Phase{State: agentphase.Stale, UpdatedAt: now - week - 1}, false, true},
		{"dead in flight kept for the watchdog", agentphase.Phase{State: agentphase.Running}, false, false},
		{"dead unread done kept", agentphase.Phase{State: agentphase.Done, UpdatedAt: now - 1000}, false, false},
		{"dead done a week old pruned", agentphase.Phase{State: agentphase.Done, UpdatedAt: now - week - 1}, false, true},
	} {
		if got := prunablePtyPhase(tc.ph, tc.alive, now); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
