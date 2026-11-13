package main

import (
	"strconv"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

func TestIsNetworkFailure(t *testing.T) {
	for _, msg := range []string{"API Error: Connection error.", "fetch failed", "stream disconnected before completion", "getaddrinfo ENOTFOUND api.anthropic.com", "socket hang up"} {
		if !isNetworkFailure(msg) {
			t.Errorf("%q should read as a network failure", msg)
		}
	}
	for _, msg := range []string{"", "Invalid API key", "prompt is too long", "You've hit your usage limit"} {
		if isNetworkFailure(msg) {
			t.Errorf("%q is not the network's fault", msg)
		}
	}
}

func TestNetResumeWaitsForNetworkThenContinues(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	store, _ := NewPhaseStore(a.db)
	a.phases = store
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	cid := strconv.FormatInt(c.ID, 10)
	a.noteNetFailure(cid, ProviderRuntimeEvent{Type: EvtTurnFailed, Message: "API Error: Connection error."}, time.Now())
	if row, _ := a.chatByID(c.ID); row.NetWaitAt == 0 || row.NetWaitReason != "network" {
		t.Fatalf("not marked: %+v", row)
	}

	online := false
	prev := netReachable
	netReachable = func() bool { return online }
	t.Cleanup(func() { netReachable = prev })
	var sent []string
	send := func(_ int64, text string, _ []string) error { sent = append(sent, text); return nil }

	a.runDeliveries(a.netResumeDeliveries(time.Now(), send, nil))
	if len(sent) != 0 {
		t.Fatal("offline: nothing may be sent")
	}
	online = true
	a.runDeliveries(a.netResumeDeliveries(time.Now(), send, nil))
	if len(sent) != 1 || sent[0] != netResumeText {
		t.Fatalf("back online: one continue prompt, got %q", sent)
	}
	// The turn it starts ends the episode.
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookRunning})
	a.clearNetWaitOnRunning(cid)
	if row, _ := a.chatByID(c.ID); row.NetWaitAt != 0 {
		t.Fatal("a running turn must clear the wait")
	}
}

func TestRestartCutTurnWaitsToContinue(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	store, _ := NewPhaseStore(a.db)
	a.phases = store
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	cid := strconv.FormatInt(c.ID, 10)
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookRunning})
	a.markRestartCutTurns()
	if a.phases.Get("chat:" + cid).InFlight() {
		t.Fatal("a turn cut by the restart must settle")
	}
	if row, _ := a.chatByID(c.ID); row.NetWaitReason != "restart" {
		t.Fatalf("not marked to continue: %+v", row)
	}
	time.Sleep(100 * time.Millisecond) // the interrupt settles its audit on a goroutine
}
