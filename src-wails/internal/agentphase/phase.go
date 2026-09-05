// Package agentphase derives an agent's phase from the facts the backend
// already has: status hooks, provider runtime events and PTY liveness.
//
// It is a pure function on purpose. The phase must be derivable with NO client
// connected — the phone is asleep, the PWA is closed, and the answer still has
// to exist. That is also why `review` is not a phase here: whether a finished
// turn still needs looking at is per-device, so it is a read receipt the client
// derives from TurnEndedAt against its own seenAt.
//
// This package must not import database/sql, the Wails runtime, or anything
// from main. IO belongs to the store.
package agentphase

type State string

const (
	Idle            State = "idle"
	Running         State = "running"
	WaitingInput    State = "waiting_input"
	WaitingApproval State = "waiting_approval"
	Done            State = "done"
	Failed          State = "failed"
	// Stale is the dead-PTY watchdog: the turn never ended, the process is
	// gone. "interrupt" named what we did about it; this names what happened.
	Stale State = "stale"
)

// Phase is comparable on purpose — the store skips writes and emits when
// Next() returns an unchanged value.
type Phase struct {
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"` // error_type, blocking tool name
	Model  string `json:"model,omitempty"`
	Title  string `json:"title,omitempty"`
	// IsAgent gates the poll channel. An agent stays foreground whether it is
	// thinking or idle at its prompt, so the poll must never speak for it.
	IsAgent     bool  `json:"is_agent"`
	TurnEndedAt int64 `json:"turn_ended_at"` // 0 while a turn is in flight
	UpdatedAt   int64 `json:"updated_at"`
}

// InFlight reports whether a turn is still open.
func (p Phase) InFlight() bool {
	return p.State == Running || p.State == WaitingInput || p.State == WaitingApproval
}

type Kind string

const (
	HookRunning    Kind = "hook_running"
	HookWaiting    Kind = "hook_waiting"
	HookPermission Kind = "hook_permission"
	HookDone       Kind = "hook_done"
	HookError      Kind = "hook_error"
	HookSession    Kind = "hook_session"

	PollAgent      Kind = "poll_agent" // Bool = isAgent
	PollBusy       Kind = "poll_busy"
	PollNotBusy    Kind = "poll_not_busy"
	PollNeedsInput Kind = "poll_needs_input"
	PollGotInput   Kind = "poll_got_input"

	Interrupt Kind = "interrupt" // Ctrl+C
	Dead      Kind = "dead"      // watchdog confirmed the PTY is gone
)

type Event struct {
	Kind   Kind
	Detail string
	Model  string
	Source string
	Title  string
	Bool   bool
}

// Next returns the phase after ev. It returns cur unchanged (UpdatedAt
// included) when nothing happened, so callers can treat equality as "no news".
func Next(cur Phase, ev Event, now int64) Phase {
	if cur.State == "" {
		cur.State = Idle
	}
	next := cur

	switch ev.Kind {
	case HookRunning:
		next.State = Running
		next.Detail = ""
		next.TurnEndedAt = 0
	case HookWaiting:
		next.State = WaitingInput
	case HookPermission:
		next.State = WaitingApproval
	case HookDone:
		if cur.State != Done {
			next.State = Done
			next.TurnEndedAt = now
		}
	case HookError:
		if cur.State != Failed || cur.Detail != ev.Detail {
			next.State = Failed
			next.Detail = ev.Detail
			next.TurnEndedAt = now
		}
	case HookSession:
		// Metadata, not a status: SessionStart labels the tab, it does not
		// start a turn.
		if ev.Model != "" {
			next.Model = ev.Model
		}
		if ev.Title != "" {
			next.Title = ev.Title
		}
	case PollAgent:
		next.IsAgent = ev.Bool
	case PollBusy:
		if !cur.IsAgent {
			next.State = Running
			next.Detail = ""
			next.TurnEndedAt = 0
		}
	case PollNotBusy:
		if !cur.IsAgent && cur.InFlight() {
			next.State = Done
			next.TurnEndedAt = now
		}
	case PollNeedsInput:
		if !cur.IsAgent && cur.State == Running {
			next.State = WaitingInput
		}
	case PollGotInput:
		if !cur.IsAgent && cur.State == WaitingInput {
			next.State = Running
		}
	case Interrupt:
		next.State = Idle
		next.Detail = ""
		next.TurnEndedAt = 0
	case Dead:
		if cur.InFlight() {
			next.State = Stale
			next.TurnEndedAt = now
		}
	}

	if next == cur {
		return cur
	}
	next.UpdatedAt = now
	return next
}
