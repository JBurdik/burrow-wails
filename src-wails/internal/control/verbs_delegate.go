package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Delegation verbs: handing work to sub-agents and supervising them.
//
// The client describes the WORK ("investigate the cache bug", agent: codex),
// never a command line. Building the argv is the frontend's job — it already
// owns the provider registry the Settings page configures, so a Manager can't
// invent a flag that doesn't exist or forget that a given agent isn't `claude`.
//
// Results come back over the file-based capture channel that already exists:
// a spawned agent's Stop hook writes <session>/<token>.result + .done, so
// wait_result/collect_results are plain file reads and survive an app restart.

// SpawnResult identifies what was opened, so the caller can supervise it.
type SpawnResult struct {
	PtyID  int64  `json:"pty_id,omitempty"`
	ChatID int64  `json:"chat_id,omitempty"`
	Token  string `json:"token,omitempty"`
	Target string `json:"target"`
}

// Result is one finished sub-agent's output.
type Result struct {
	Token string `json:"token"`
	Text  string `json:"text"`
}

func delegationVerbs(c *Core) []Verb {
	return []Verb{{
		Name:    "spawn",
		Summary: "Delegate a task to a sub-agent in a new tab (or chat), visible to the user",
		Args: []Arg{
			{Name: "task", Type: "string", Desc: "The full task prompt for the sub-agent: what to do, what not to touch, what to report", Required: true},
			{Name: "agent", Type: "string", Desc: "Agent instance to run (name or id from list_agents); defaults to the user's default"},
			{Name: "model", Type: "string", Desc: "Model override for this task, e.g. claude-haiku-4-5-20251001 for mechanical work"},
			{Name: "cwd", Type: "string", Desc: "Directory to run in — a worktree path for isolated work; defaults to the caller's"},
			{Name: "target", Type: "string", Desc: "tab (default; live terminal, result capture) or chat (structured, in the sidebar)"},
			{Name: "capture", Type: "boolean", Desc: "Capture the agent's final message for wait_result (tab target only, default true)"},
			{Name: "parent_chat_id", Type: "integer", Desc: "Set automatically from BURROW_CHAT_ID — the thread this sub-agent belongs to"},
			{Name: "caller_is_subagent", Type: "boolean", Desc: "Set automatically — a sub-agent may not spawn further sub-agents"},
			{Name: "parent_is_control", Type: "boolean", Desc: "Set automatically — a Manager spawn is exempt from the parent-forces-chat rule"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.spawn(ctx, p) },
	}, {
		Name:    "list_agents",
		Summary: "Agent instances configured in Settings > Providers, spawnable by name",
		Scope:   ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			var out any
			return out, c.ui(ctx, "list_agents", nil, &out)
		},
	}, {
		Name:    "agent_status",
		Summary: "Live status of every agent in the app: running, waiting, permission, review, done, idle",
		Scope:   ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			var out any
			return out, c.ui(ctx, "agent_status", nil, &out)
		},
	}, {
		Name:    "tab_output",
		Summary: "Read the tail of a tab's terminal output — how you check on an agent mid-task",
		Args: []Arg{
			{Name: "pty_id", Type: "integer", Desc: "Tab to read", Required: true},
			{Name: "lines", Type: "integer", Desc: "How many trailing lines (default 80, max 500)"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			lines := p.Int("lines")
			if lines <= 0 {
				lines = 80
			}
			if lines > 500 {
				lines = 500
			}
			var out any
			return out, c.ui(ctx, "tab_output", map[string]any{"ptyId": p.Int("pty_id"), "lines": lines}, &out)
		},
	}, {
		Name:    "send_to_tab",
		Summary: "Type a follow-up message into a running agent's tab and submit it",
		Args: []Arg{
			{Name: "pty_id", Type: "integer", Desc: "Tab to send to", Required: true},
			{Name: "text", Type: "string", Desc: "Message to send", Required: true},
			{Name: "submit", Type: "boolean", Desc: "Press Enter after typing (default true)"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.sendToTab(p) },
	}, {
		Name:    "chat_send",
		Summary: "Send a follow-up message to a chat sub-agent and submit it",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Chat to send to — a sub-agent id from agent_status", Required: true},
			{Name: "text", Type: "string", Desc: "Message to send", Required: true},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			if p.Int("chat_id") <= 0 {
				return nil, fmt.Errorf("chat_send needs a chat_id")
			}
			if strings.TrimSpace(p.Str("text")) == "" {
				return nil, fmt.Errorf("chat_send needs text")
			}
			var out any
			return out, c.ui(ctx, "chat_send", map[string]any{"chatId": p.Int("chat_id"), "text": p.Str("text")}, &out)
		},
	}, {
		Name:    "wait_result",
		Summary: "Block until a spawned agent finishes and return its final message",
		Args: []Arg{
			{Name: "token", Type: "string", Desc: "Token returned by spawn"},
			{Name: "chat_id", Type: "integer", Desc: "Chat sub-agent to wait on, instead of a token"},
			{Name: "timeout", Type: "integer", Desc: "Seconds to wait (default 600)"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.waitResult(ctx, p) },
	}, {
		Name:    "collect_results",
		Summary: "Take every finished sub-agent result that hasn't been collected yet",
		Args: []Arg{
			{Name: "parent_chat_id", Type: "integer", Desc: "Set automatically from BURROW_CHAT_ID — sweeps this thread's finished chat sub-agents too"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.collectResults(p) },
	}}
}

func (c *Core) spawn(ctx context.Context, p Params) (any, error) {
	if p.Bool("caller_is_subagent") {
		return nil, fmt.Errorf("sub-agent cannot spawn sub-agents")
	}
	parent := p.Int("parent_chat_id")

	target := p.Str("target")
	if target == "" {
		target = "tab"
	}
	// A sub-agent that belongs to a thread IS a chat: a terminal tab would put
	// it back in the Sidebar as a peer, which is the arrangement this replaces.
	//
	// EXEMPT: a `control` chat (the per-repo Manager — see ManagerPanel.vue).
	// The Manager is control:true and never the active session, and the Right
	// Panel's Sub-agents list is scoped to the active session, so a
	// Manager-spawned CHAT sub-agent would land in no list at all: not the
	// Sidebar (filtered out as a sub-agent), not the panel (wrong thread). A
	// Manager spawn instead keeps behaving exactly as it does on `main` — a
	// terminal tab, with result capture, `tab_output` and `send_to_tab` — which
	// is also what its primer (managerPrimer.ts) still promises. Derived
	// server-side (parent_is_control), never trusted from the request, same as
	// caller_is_subagent just above.
	if parent > 0 && !p.Bool("parent_is_control") {
		target = "chat"
	}
	if target != "tab" && target != "chat" {
		return nil, fmt.Errorf("spawn: target must be tab or chat, got %q", target)
	}

	// A chat sub-agent has no Stop hook of ours to run, so there is nothing to
	// capture — say so rather than handing back a token that never resolves.
	capture := target == "tab" && (p["capture"] == nil || p.Bool("capture"))
	token := ""
	if capture {
		token = fmt.Sprintf("res%d", time.Now().UnixNano())
	}

	args := map[string]any{
		"task":           p.Str("task"),
		"agent":          p.Str("agent"),
		"model":          p.Str("model"),
		"cwd":            p.Str("cwd"),
		"target":         target,
		"token":          token,
		"parent_chat_id": parent,
	}
	var res SpawnResult
	if err := c.ui(ctx, "spawn", args, &res); err != nil {
		return nil, err
	}
	res.Target, res.Token = target, token
	return res, nil
}

func (c *Core) sendToTab(p Params) (any, error) {
	if c.deps.PTY == nil {
		return nil, fmt.Errorf("send_to_tab: no terminal backend")
	}
	text := p.Str("text")
	if p["submit"] == nil || p.Bool("submit") {
		text += "\r"
	}
	if err := c.deps.PTY.WritePty(p.Str("pty_id"), text); err != nil {
		return nil, fmt.Errorf("send_to_tab: %w", err)
	}
	return map[string]any{"sent": true}, nil
}

// waitResult polls for the capture file, or — for a chat sub-agent — for the
// phase Go already derives. Polling (not inotify) because the writer is a
// hook (or the provider-runtime parser) in another process and the wait is
// measured in minutes — a 500ms tick is free at that scale and has no watcher
// lifecycle to leak.
func (c *Core) waitResult(ctx context.Context, p Params) (any, error) {
	token := p.Str("token")
	chatID := p.Int("chat_id")
	if token == "" && chatID <= 0 {
		return nil, fmt.Errorf("wait_result needs a token or a chat_id")
	}
	// Both capabilities are needed for the chat_id branch below; check once,
	// up front, rather than dereferencing a possibly-nil interface every poll.
	if chatID > 0 && (c.deps.Phases == nil || c.deps.Chats == nil) {
		return nil, fmt.Errorf("wait_result: chat_id needs Phases and Chats wired into Deps")
	}
	timeout := time.Duration(p.Int("timeout")) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waitStart := time.Now()
	deadline := waitStart.Add(timeout)

	// The documented workflow is chat_send then wait_result --chat-id, but
	// chat_send does not always move the phase to `running` synchronously —
	// the CLI has to pick the prompt up first. Without a baseline, a phase
	// left `done` by the PREVIOUS turn (or one that was already idle before
	// this wait even started) reads as "finished" on the very first poll
	// below, so wait_result returns the previous turn's stale answer
	// immediately and marks the child collected — a caller never sees its
	// own turn's result. Capturing turn_ended_at up front and requiring it
	// to ADVANCE is what a spawned token already gets for free (a token is
	// per-spawn, so it has no previous answer to be confused with); this
	// gives a chat the same guarantee.
	var baselineTurnEndedAt int64
	if chatID > 0 {
		_, baselineTurnEndedAt = c.deps.Phases.Phase(fmt.Sprintf("chat:%d", chatID))
	}

	finishChat := func() (any, error) {
		text, err := c.deps.Chats.LastAssistantMessage(chatID)
		if err != nil {
			return nil, err
		}
		_ = c.deps.Chats.MarkCollected(chatID)
		return Result{Token: fmt.Sprintf("chat:%d", chatID), Text: text}, nil
	}

	for {
		if chatID > 0 {
			// A chat writes no capture files; the phase Go derives IS the
			// completion signal, and it is derived with no client attached.
			state, turnEndedAt := c.deps.Phases.Phase(fmt.Sprintf("chat:%d", chatID))
			switch {
			case state == "stale":
				// Dead process, not a turn boundary — chat_send can never
				// move a dead pipe's phase to Running, so requiring an
				// ADVANCE here (the rule below is for done/failed) would
				// only turn "instantly wrong" into "block the whole
				// timeout, then wrong anyway". Return the moment it's seen,
				// baseline or not — this used to answer immediately and
				// must keep doing so.
				return finishChat()
			case state == "done" || state == "failed":
				// The turn genuinely advanced past the baseline: definitely
				// this call's own answer. OR: the baseline grace window has
				// elapsed with the phase never budging — the common case for
				// a fast child whose first (and only) turn already finished
				// before this wait's baseline was even captured, so there
				// was never a "previous" turn to confuse it with; blocking
				// such a call for the whole (often 10-minute) timeout is
				// worse than the small chance of an early answer. A chat
				// mid-race with a JUST-sent chat_send whose CLI is slow to
				// start typically clears this by advancing turn_ended_at (or
				// passing through running/waiting) well inside the grace
				// window — see TestWaitResult* for both shapes.
				if turnEndedAt > baselineTurnEndedAt || time.Since(waitStart) >= waitResultBaselineGrace {
					return finishChat()
				}
			}
		} else if text, ok := c.takeResult(token); ok {
			return Result{Token: token, Text: text}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("wait_result: %s did not finish within %s (it may still be working — check agent_status)", firstNonEmpty(token, fmt.Sprintf("chat:%d", chatID)), timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// waitResultBaselineGrace bounds how long waitResult will hold a chat's
// answer back solely because turn_ended_at hasn't advanced past its baseline
// (see baselineTurnEndedAt in waitResult) — after this, a done/failed phase
// is trusted even without an observed advance. A var, not a const, so a test
// can shrink it instead of sleeping for the real duration.
var waitResultBaselineGrace = 3 * time.Second

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (c *Core) collectResults(p Params) (any, error) {
	out := []Result{}
	// Chat sub-agents first: they write no .done file, so collected_at is what
	// stops this from returning the same answer on every call.
	if parent := p.Int("parent_chat_id"); parent > 0 && c.deps.Chats != nil && c.deps.Phases != nil {
		ids, err := c.deps.Chats.UncollectedChildren(parent)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			state, _ := c.deps.Phases.Phase(fmt.Sprintf("chat:%d", id))
			if state != "done" && state != "failed" && state != "stale" {
				continue // still working — not a result yet
			}
			text, err := c.deps.Chats.LastAssistantMessage(id)
			if err != nil {
				return out, err
			}
			_ = c.deps.Chats.MarkCollected(id)
			out = append(out, Result{Token: fmt.Sprintf("chat:%d", id), Text: text})
		}
	}

	if c.deps.SessionDir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(c.deps.SessionDir)
	if err != nil {
		return out, nil // no session dir yet = nothing has ever been spawned
	}
	tokens := []string{}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".done") {
			tokens = append(tokens, strings.TrimSuffix(name, ".done"))
		}
	}
	sort.Strings(tokens) // tokens are time-ordered, so this is chronological

	for _, t := range tokens {
		if text, ok := c.takeResult(t); ok {
			out = append(out, Result{Token: t, Text: text})
		}
	}
	return out, nil
}

// takeResult reads a finished result and deletes its marker files, so the same
// result is never handed out twice — collect_results is a queue, not a log.
func (c *Core) takeResult(token string) (string, bool) {
	if c.deps.SessionDir == "" || token == "" {
		return "", false
	}
	base := filepath.Join(c.deps.SessionDir, token)
	if _, err := os.Stat(base + ".done"); err != nil {
		return "", false
	}
	text, _ := os.ReadFile(base + ".result")
	_ = os.Remove(base + ".done")
	_ = os.Remove(base + ".result")
	return strings.TrimSpace(string(text)), true
}
