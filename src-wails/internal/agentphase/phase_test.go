package agentphase

import "testing"

const now = int64(1_000)

func apply(p Phase, evs ...Event) Phase {
	for _, ev := range evs {
		p = Next(p, ev, now)
	}
	return p
}

func TestStartsIdle(t *testing.T) {
	var p Phase
	if p.State != "" && p.State != Idle {
		t.Fatalf("zero value is not idle: %q", p.State)
	}
	if got := Next(p, Event{Kind: HookRunning}, now); got.State != Running {
		t.Fatalf("idle → running failed: %q", got.State)
	}
}

func TestHookTransitions(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning})
	if p.State != Running {
		t.Fatalf("want running, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookWaiting}); p.State != WaitingInput {
		t.Fatalf("want waiting_input, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookPermission}); p.State != WaitingApproval {
		t.Fatalf("want waiting_approval, got %q", p.State)
	}
	if p = apply(p, Event{Kind: HookRunning}); p.State != Running {
		t.Fatalf("resume failed, got %q", p.State)
	}
}

func TestPermissionFromIdle(t *testing.T) {
	// A native app-server agent can deliver an approval RPC before its first
	// visible output; it is still actionable.
	if got := apply(Phase{}, Event{Kind: HookPermission}); got.State != WaitingApproval {
		t.Fatalf("want waiting_approval, got %q", got.State)
	}
}

func TestDoneRecordsTurnEnd(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookDone})
	if p.State != Done {
		t.Fatalf("want done, got %q", p.State)
	}
	if p.TurnEndedAt != now {
		t.Fatalf("turn end not recorded: %d", p.TurnEndedAt)
	}
}

func TestFailCarriesDetailAndClearsOnNewTurn(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookError, Detail: "billing_error"})
	if p.State != Failed || p.Detail != "billing_error" {
		t.Fatalf("want failed/billing_error, got %q/%q", p.State, p.Detail)
	}
	if p.TurnEndedAt != now {
		t.Fatalf("failed turn must record its end: %d", p.TurnEndedAt)
	}
	p = apply(p, Event{Kind: HookRunning})
	if p.State != Running || p.Detail != "" {
		t.Fatalf("new turn must clear detail: %q/%q", p.State, p.Detail)
	}
}

func TestSessionIsMetadataNotStatus(t *testing.T) {
	p := apply(Phase{}, Event{Kind: HookRunning})
	got := apply(p, Event{Kind: HookSession, Model: "opus", Title: "Fix the parser"})
	if got.State != Running {
		t.Fatalf("session must not change state: %q", got.State)
	}
	if got.Model != "opus" || got.Title != "Fix the parser" {
		t.Fatalf("session metadata lost: %+v", got)
	}
}

func TestPollNeverDrivesAnAgent(t *testing.T) {
	// The whole "stuck orange dot" rule: an agent is foreground whether it is
	// thinking or sitting at its prompt, so presence is not busy.
	agent := apply(Phase{}, Event{Kind: PollAgent, Bool: true})
	if got := apply(agent, Event{Kind: PollBusy}); got.State == Running {
		t.Fatal("poll fabricated running for an agent leaf")
	}
	live := apply(agent, Event{Kind: HookRunning})
	if got := apply(live, Event{Kind: PollNotBusy}); got.State != Running {
		t.Fatalf("poll settled a live agent turn: %q", got.State)
	}
	if got := apply(live, Event{Kind: PollNeedsInput}); got.State != Running {
		t.Fatalf("poll dragged a running agent into waiting: %q", got.State)
	}
}

func TestPollDrivesPlainCommands(t *testing.T) {
	p := apply(Phase{}, Event{Kind: PollBusy})
	if p.State != Running {
		t.Fatalf("want running, got %q", p.State)
	}
	if got := apply(p, Event{Kind: PollNeedsInput}); got.State != WaitingInput {
		t.Fatalf("want waiting_input, got %q", got.State)
	}
	waiting := apply(p, Event{Kind: PollNeedsInput})
	if got := apply(waiting, Event{Kind: PollGotInput}); got.State != Running {
		t.Fatalf("want running, got %q", got.State)
	}
	if got := apply(waiting, Event{Kind: PollNotBusy}); got.State != Done {
		t.Fatalf("a waiting command that exits must still settle: %q", got.State)
	}
}

func TestNeedsInputAtIdlePromptIsNoop(t *testing.T) {
	if got := apply(Phase{}, Event{Kind: PollNeedsInput}); got.State != Idle && got.State != "" {
		t.Fatalf("idle prompt produced a dot: %q", got.State)
	}
}

func TestSetAgentFlipsTheGuardMidFlight(t *testing.T) {
	p := apply(Phase{}, Event{Kind: PollBusy})
	p = apply(p, Event{Kind: PollAgent, Bool: true})
	if got := apply(p, Event{Kind: PollNotBusy}); got.State != Running {
		t.Fatalf("guard did not flip: %q", got.State)
	}
}

func TestInterruptSettlesToIdle(t *testing.T) {
	for _, start := range []Kind{HookRunning, HookWaiting, HookPermission} {
		p := apply(Phase{}, Event{Kind: start})
		if got := apply(p, Event{Kind: Interrupt}); got.State != Idle {
			t.Fatalf("interrupt from %q left %q", start, got.State)
		}
	}
}

func TestDeadOnlySettlesInFlight(t *testing.T) {
	live := apply(Phase{}, Event{Kind: HookRunning})
	if got := apply(live, Event{Kind: Dead}); got.State != Stale {
		t.Fatalf("want stale, got %q", got.State)
	}
	finished := apply(Phase{}, Event{Kind: HookRunning}, Event{Kind: HookDone})
	if got := apply(finished, Event{Kind: Dead}); got.State != Done {
		t.Fatalf("dead must not overwrite a finished turn: %q", got.State)
	}
}

func TestNoopReturnsAnUnchangedPhase(t *testing.T) {
	// The store relies on this to skip a write and an emit.
	p := apply(Phase{}, Event{Kind: HookRunning})
	if got := Next(p, Event{Kind: HookRunning}, now+5); got != p {
		t.Fatalf("repeated event produced a change: %+v vs %+v", got, p)
	}
}
