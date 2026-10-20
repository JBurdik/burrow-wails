package main

import (
	"testing"
	"time"

	"burrow/internal/agentphase"
)

func TestChatSleepEligible(t *testing.T) {
	now := time.Unix(10_000, 0)
	idle := 30 * time.Minute
	old, fresh := now.Add(-31*time.Minute), now.Add(-29*time.Minute)
	cases := []struct {
		name  string
		state agentphase.State
		busy  bool
		last  time.Time
		idle  time.Duration
		want  bool
	}{
		{"settled done, stale", agentphase.Done, false, old, idle, true},
		{"idle, stale", agentphase.Idle, false, old, idle, true},
		{"failed, stale", agentphase.Failed, false, old, idle, true},
		{"stale phase, stale", agentphase.Stale, false, old, idle, true},
		{"exactly at threshold", agentphase.Done, false, now.Add(-idle), idle, true},
		{"recent activity", agentphase.Done, false, fresh, idle, false},
		{"running turn", agentphase.Running, false, old, idle, false},
		{"pending permission", agentphase.WaitingApproval, false, old, idle, false},
		{"pending question", agentphase.WaitingInput, false, old, idle, false},
		{"transport still has a turn open", agentphase.Done, true, old, idle, false},
		{"no phase known", "", false, old, idle, false},
		{"setting off", agentphase.Done, false, old, 0, false},
	}
	for _, c := range cases {
		if got := chatSleepEligible(c.state, c.busy, c.last, now, c.idle); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The sweep with an injected clock: a settled chat sleeps only once the idle
// window has passed since its last activity, a blocked one never does, a
// watching client's ping keeps it awake, and the next send wakes it with
// --resume and clears slept_at.
func TestSleepIdleChatsSweep(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})

	settled, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	blocked, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	for _, c := range []Chat{settled, blocked} {
		if err := a.ChatSend(c.ID, "hi", nil); err != nil {
			t.Fatal(err)
		}
	}
	fake.onLine["1"](`{"type":"system","subtype":"init","session_id":"sess-1"}`)
	a.phases.Apply("chat:1", agentphase.Event{Kind: agentphase.HookRunning})
	a.phases.Apply("chat:1", agentphase.Event{Kind: agentphase.HookDone})
	a.phases.Apply("chat:2", agentphase.Event{Kind: agentphase.HookRunning})
	a.phases.Apply("chat:2", agentphase.Event{Kind: agentphase.HookPermission})

	idle := 60 * time.Minute
	start := time.Now()

	if got := a.sleepIdleChats(start.Add(59*time.Minute), idle); len(got) != 0 {
		t.Fatalf("slept before the idle window passed: %v", got)
	}

	// A watching client pings at +50m: the window restarts from there.
	a.chatRT.touchChat("1", start.Add(50*time.Minute))
	if got := a.sleepIdleChats(start.Add(100*time.Minute), idle); len(got) != 0 {
		t.Fatalf("slept a watched chat: %v", got)
	}

	got := a.sleepIdleChats(start.Add(111*time.Minute), idle)
	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("slept = %v, want [1] (chat 2 waits on a permission)", got)
	}
	if fake.Alive("1") || !fake.Alive("2") {
		t.Fatalf("alive: 1=%v 2=%v, want false/true", fake.Alive("1"), fake.Alive("2"))
	}
	row, _ := a.chatByID(settled.ID)
	if row.SleptAt == 0 {
		t.Fatal("slept chat has no slept_at")
	}
	if a.phases.Get("chat:1").State != agentphase.Done {
		t.Fatalf("sleep changed the phase to %q; the review dot must survive", a.phases.Get("chat:1").State)
	}

	// Sending wakes it: a resumed start, stamp cleared.
	if err := a.ChatSend(settled.ID, "again", nil); err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(fake.lastStart().args, "--resume", "sess-1") {
		t.Fatalf("wake did not resume: %v", fake.lastStart().args)
	}
	if row, _ := a.chatByID(settled.ID); row.SleptAt != 0 {
		t.Fatal("slept_at not cleared by the waking send")
	}
}

// Manual settle sleeps a settled chat at once, but never a running one.
func TestChatSleepManual(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})

	settled, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	running, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	for _, c := range []Chat{settled, running} {
		if err := a.ChatSend(c.ID, "hi", nil); err != nil {
			t.Fatal(err)
		}
	}
	a.phases.Apply("chat:1", agentphase.Event{Kind: agentphase.HookRunning})
	a.phases.Apply("chat:1", agentphase.Event{Kind: agentphase.HookDone})
	a.phases.Apply("chat:2", agentphase.Event{Kind: agentphase.HookRunning})

	if a.ChatSleep(running.ID) || !fake.Alive("2") {
		t.Fatal("slept a running chat")
	}
	if !a.ChatSleep(settled.ID) || fake.Alive("1") {
		t.Fatal("settled chat not slept")
	}
	if row, _ := a.chatByID(settled.ID); row.SleptAt == 0 {
		t.Fatal("no slept_at")
	}
}

func TestChatAutoSleepSetting(t *testing.T) {
	a, _, _, _ := runtimeApp(t)
	writeConfig(t, map[string]any{})
	if got := a.chatAutoSleepIdle(); got != 60*time.Minute {
		t.Fatalf("default = %v, want 60m", got)
	}
	writeConfig(t, map[string]any{"uiPrefs": map[string]any{"chatAutoSleepMinutes": 0}})
	if got := a.chatAutoSleepIdle(); got != 0 {
		t.Fatalf("off = %v, want 0", got)
	}
	writeConfig(t, map[string]any{"uiPrefs": map[string]any{"chatAutoSleepMinutes": 15}})
	if got := a.chatAutoSleepIdle(); got != 15*time.Minute {
		t.Fatalf("15 = %v", got)
	}
}
