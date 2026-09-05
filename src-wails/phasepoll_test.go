package main

import (
	"testing"

	"burrow/internal/agentphase"
)

type fakePty struct {
	sessions []string
	fg       map[string]string
}

func (f *fakePty) list() ([]string, error) { return f.sessions, nil }
func (f *fakePty) foreground(id string) string {
	return f.fg[id]
}

func TestPollMarksAgentLeaves(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": "claude"}}
	p := newPhasePoller(s, f.list, f.foreground)

	p.tick()

	if !s.Get("pty:7").IsAgent {
		t.Fatal("agent foreground did not set IsAgent")
	}
	if s.Get("pty:7").State == agentphase.Running {
		t.Fatal("the poll fabricated running for an agent — hooks are the sole authority")
	}
}

func TestPollDrivesPlainCommands(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": "npm"}}
	p := newPhasePoller(s, f.list, f.foreground)

	p.tick()
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("plain command did not go running: %+v", s.Get("pty:7"))
	}

	f.fg["7"] = "zsh" // back at the prompt
	p.tick()
	if s.Get("pty:7").State != agentphase.Done {
		t.Fatalf("returning to the shell did not settle: %+v", s.Get("pty:7"))
	}
}

func TestWatchdogNeedsThreeEmptyReadsAndADeadPty(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": ""}}
	p := newPhasePoller(s, f.list, f.foreground)

	for i := 0; i < 5; i++ {
		p.tick()
	}
	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("an empty foreground on a LIVE pty settled the dot: %+v", s.Get("pty:7"))
	}

	f.sessions = nil // the daemon confirms it is gone
	p.tick()
	if s.Get("pty:7").State != agentphase.Stale {
		t.Fatalf("dead pty did not go stale: %+v", s.Get("pty:7"))
	}
}

func TestWatchdogIgnoresASingleEmptyRead(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	f := &fakePty{sessions: nil, fg: map[string]string{"7": ""}}
	p := newPhasePoller(s, f.list, f.foreground)
	p.tick()

	if s.Get("pty:7").State != agentphase.Running {
		t.Fatalf("one empty read is a daemon race, not a death: %+v", s.Get("pty:7"))
	}
}
