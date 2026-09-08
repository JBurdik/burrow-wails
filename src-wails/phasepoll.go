package main

import (
	"context"
	"regexp"
	"sync"
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
	// mu guards empty, which the ticker goroutine writes and CreatePty's
	// goroutine clears through forget().
	mu    sync.Mutex
	empty map[string]int
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
			seen[id] = true
			p.pollOne(id, true)
		}
	}

	// Drop the watchdog counters of ptys nobody polls any more (their phase was
	// forgotten, and the daemon no longer lists them). Without this the map is
	// the one thing in the poller that only ever grows.
	p.mu.Lock()
	for id := range p.empty {
		if !seen[id] {
			delete(p.empty, id)
		}
	}
	p.mu.Unlock()
}

// forget clears the watchdog counter for one pty. Called when a pty id is
// reused by a fresh spawn, so the new tab does not inherit the dead one's
// empty-read streak and trip the watchdog on its first tick.
func (p *phasePoller) forget(id string) {
	p.mu.Lock()
	delete(p.empty, id)
	p.mu.Unlock()
}

func (p *phasePoller) pollOne(id string, alive bool) {
	key := "pty:" + id
	// A pty the daemon does not list has no foreground to ask about, and the
	// daemon answers an unknown id with an error it LOGS — so asking anyway
	// printed a line per dead phase row per tick, forever. Not-listed is
	// itself the empty read: the watchdog wants a pty that is both quiet and
	// unlisted, and `alive` already settles the second half. Patience is
	// unchanged (emptyReadsBeforeDead ticks), so a transient daemon race that
	// briefly drops the pty from `list` still cannot settle a live turn.
	name := ""
	if alive {
		name = p.fg(id)
	}

	if name == "" {
		p.mu.Lock()
		p.empty[id]++
		streak := p.empty[id]
		p.mu.Unlock()
		if streak >= emptyReadsBeforeDead && !alive {
			// Idempotent past the first one: Next() returns cur unchanged for a
			// phase that is already Stale (or was never InFlight, where Dead is
			// a no-op), so Apply writes nothing and emits nothing.
			p.phases.Apply(key, agentphase.Event{Kind: agentphase.Dead})
		}
		return
	}
	p.mu.Lock()
	p.empty[id] = 0
	p.mu.Unlock()

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
// side because the phase must be derivable with no client attached. It returns
// the poller so CreatePty can clear a reused id's watchdog counter.
func startPhasePoll(ctx context.Context, ps *PhaseStore, list func() ([]string, error), fg func(string) string) *phasePoller {
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
	return p
}
