package main

import (
	"context"
	"regexp"
	"time"

	"burrow/internal/agentphase"
)

// shellRE matches an interactive shell sitting at its prompt. The daemon
// reports the shell BY NAME when it is foreground — that is how we learn a
// command or agent has exited.
var shellRE = regexp.MustCompile(`^(zsh|bash|sh|fish|csh|tcsh|dash)$`)

// agentRE matches the CLIs we treat as agents. For an agent leaf the poll only
// ever sets IsAgent: an agent is foreground whether it is thinking or idle at
// its prompt, so presence is not busy (the old stuck-orange-dot bug).
//
// This list is a FALLBACK for an agent that has not fired a status hook yet —
// any Hook* event sets IsAgent by itself (agentphase.Next), so a CLI missing
// from here is no longer invisible, just late.
var agentRE = regexp.MustCompile(`^(claude|codex|copilot|aider|gemini|opencode|amp|goose)$`)

// emptyReadsBeforeDead is the watchdog's patience. One empty foreground read is
// a transient race with the daemon; three plus a pty the daemon no longer
// lists is a process that died without a Stop hook.
const emptyReadsBeforeDead = 3

type phasePoller struct {
	phases *PhaseStore
	list   func() ([]string, error)
	fg     func(string) string
	empty  map[string]int
}

func newPhasePoller(ps *PhaseStore, list func() ([]string, error), fg func(string) string) *phasePoller {
	return &phasePoller{phases: ps, list: list, fg: fg, empty: make(map[string]int)}
}

func (p *phasePoller) tick() {
	ids, err := p.list()
	if err != nil {
		return
	}
	alive := make(map[string]bool, len(ids))
	for _, id := range ids {
		alive[id] = true
	}

	// Every pty the store knows about, not just the live ones: a pty that just
	// died is exactly the case the watchdog exists for.
	seen := make(map[string]bool)
	for key := range p.phases.All() {
		id, ok := cutPtyKey(key)
		if !ok {
			continue
		}
		seen[id] = true
		p.pollOne(id, alive[id])
	}
	for _, id := range ids {
		if !seen[id] {
			p.pollOne(id, true)
		}
	}
}

func (p *phasePoller) pollOne(id string, alive bool) {
	key := "pty:" + id
	name := p.fg(id)

	if name == "" {
		p.empty[id]++
		if p.empty[id] >= emptyReadsBeforeDead && !alive {
			p.phases.Apply(key, agentphase.Event{Kind: agentphase.Dead})
		}
		return
	}
	p.empty[id] = 0

	switch {
	case agentRE.MatchString(name):
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: true})
	case shellRE.MatchString(name):
		// Back at the prompt: whatever ran is over. This also rescues an agent
		// the user Ctrl+C'd, which fires no Stop hook.
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: false})
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollNotBusy})
	default:
		// A non-shell child INSIDE a live agent session — the agent opened a
		// pager, ran git, spawned node. Only the shell branch may clear the
		// agent flag, because only the shell being foreground proves the agent
		// is gone. Clearing it here would un-gate PollBusy on the very next
		// line and overwrite a done-but-unseen turn with a permanent `running`,
		// wiping its TurnEndedAt (and the review dot) along the way — and it
		// would flap a DB write plus an emit every 2 s while the child lives.
		if p.phases.Get(key).IsAgent {
			return
		}
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollAgent, Bool: false})
		p.phases.Apply(key, agentphase.Event{Kind: agentphase.PollBusy})
	}
}

func cutPtyKey(key string) (string, bool) {
	const prefix = "pty:"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):], true
	}
	return "", false
}

// startPhasePoll runs the poll for the life of the app. It lives on the server
// side because the phase must be derivable with no client attached.
func startPhasePoll(ctx context.Context, ps *PhaseStore, list func() ([]string, error), fg func(string) string) {
	p := newPhasePoller(ps, list, fg)
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.tick()
			}
		}
	}()
}
