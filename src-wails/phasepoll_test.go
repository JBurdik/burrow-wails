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

// TestPollIgnoresAChildOfALiveAgent covers the flapping bug: an agent's tool
// subprocess (git, a pager, node) becomes the foreground process group leader
// while the leaf is done-but-unseen. Reading that name as "not an agent" would
// strip the flag, let PollBusy through, and replace the review dot with a
// permanent orange `running` — plus a DB write and an emit every 2 s.
func TestPollIgnoresAChildOfALiveAgent(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})
	s.Apply("pty:7", agentphase.Event{Kind: agentphase.HookDone})
	before := s.Get("pty:7")

	var emits int
	busSubscribe(func(shellEvent) { emits++ })

	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": "git"}}
	p := newPhasePoller(s, f.list, f.foreground)
	p.tick()
	p.tick()

	if got := s.Get("pty:7"); got != before {
		t.Fatalf("a child of a live agent moved the phase: %+v want %+v", got, before)
	}
	if emits != 0 {
		t.Fatalf("the poll flapped %d emit(s) on an agent's child process", emits)
	}

	// The shell coming back IS proof the agent is gone: the flag clears there.
	f.fg["7"] = "zsh"
	p.tick()
	if s.Get("pty:7").IsAgent {
		t.Fatal("returning to the shell did not clear the agent flag")
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

// TestPollPrunesWatchdogCounters: empty is the one map in the poller that only
// ever grew — every pty that ever went quiet kept an entry for the life of the
// process. A pty that is neither known to the store nor listed by the daemon
// is nobody's business any more.
func TestPollPrunesWatchdogCounters(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	s, _ := newTestStore(t)
	f := &fakePty{sessions: []string{"7"}, fg: map[string]string{"7": ""}}
	p := newPhasePoller(s, f.list, f.foreground)

	p.tick()
	if p.empty["7"] != 1 {
		t.Fatalf("empty read not counted: %v", p.empty)
	}

	// The pty is reaped: gone from the daemon, and its phase forgotten with it.
	f.sessions = nil
	s.Forget("pty:7")
	p.tick()
	if _, ok := p.empty["7"]; ok {
		t.Fatalf("a reaped pty kept its watchdog counter: %v", p.empty)
	}

	// forget() is the same cleanup from CreatePty's side, for a reused id: the
	// new tab must not inherit the dead one's empty-read streak.
	p.empty["9"] = 2
	p.forget("9")
	if _, ok := p.empty["9"]; ok {
		t.Fatal("forget did not clear the counter")
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
