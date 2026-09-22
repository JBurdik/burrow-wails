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
		Args: []Arg{
			{Name: "only_children", Type: "boolean", Desc: "When true, list only this caller's sub-agents (auto-detected from BURROW_CHAT_ID; if unset, returns all agents). Tab-target spawns without a parent column are omitted from this filtered view"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			onlyChildren := p.Bool("only_children")
			parentChatID := p.Int("parent_chat_id")
			var out any
			args := map[string]any{"onlyChildren": onlyChildren}
			if parentChatID > 0 {
				args["parent_chat_id"] = parentChatID
			}
			return out, c.ui(ctx, "agent_status", args, &out)
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
		Name:    "close_chat",
		Summary: "Delete a chat sub-agent and any children spawned under it. Destructive — confirm with the user first",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Chat to delete — a sub-agent id from agent_status", Required: true},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.closeChat(p) },
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
	if parent > 0 {
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

// closeChat stops a chat sub-agent before deleting its transcript (and, by
// cascade, its children). Deleting only the database row used to leave the
// agent CLI and its MCP children alive, consuming memory with no reachable UI
// session. No UI round-trip is needed: both runtime ownership and chat
// ownership live in the host app (unlike tab_close, which reaches a UI PTY).
func (c *Core) closeChat(p Params) (any, error) {
	if c.deps.Chats == nil {
		return nil, fmt.Errorf("close_chat: no chat backend")
	}
	chatID := p.Int("chat_id")
	if chatID <= 0 {
		return nil, fmt.Errorf("close_chat needs a chat_id")
	}
	if c.deps.ChatStopper != nil {
		if err := c.deps.ChatStopper.StopChat(chatID); err != nil {
			return nil, fmt.Errorf("close_chat: stop runtime: %w", err)
		}
	}
	if err := c.deps.Chats.DeleteChat(chatID); err != nil {
		return nil, fmt.Errorf("close_chat: %w", err)
	}
	return map[string]any{"closed": chatID}, nil
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

	// Whether this wait has EVER observed the phase in flight
	// (running/waiting_input/waiting_approval). This is evidence a new turn
	// genuinely started, as opposed to a clock running out — a fixed grace
	// window here is wrong: chat_send does not move the phase to `running`
	// synchronously, and a CLI can take well past a few seconds to start
	// producing output (cold start, model queueing, a busy machine), which
	// is the common case, not an edge one. A time-based escape fired mid
	// cold-start and handed back the PREVIOUS turn's answer — the exact bug
	// this baseline rule exists to prevent — plus MarkCollected, hiding the
	// real answer from collect_results once it actually lands. Once
	// in-flight has been seen, there is no more excuse: an advance past the
	// baseline is required, with no time limit other than the caller's own
	// `deadline` below.
	seenInFlight := false

	for {
		if chatID > 0 {
			// A chat writes no capture files; the phase Go derives IS the
			// completion signal, and it is derived with no client attached.
			state, turnEndedAt := c.deps.Phases.Phase(fmt.Sprintf("chat:%d", chatID))
			switch state {
			case "running", "waiting_input", "waiting_approval":
				seenInFlight = true
			case "stale":
				// Dead process, not a turn boundary — chat_send can never
				// move a dead pipe's phase to Running, so requiring an
				// ADVANCE here (the rule below is for done/failed) would
				// only turn "instantly wrong" into "block the whole
				// timeout, then wrong anyway". Return the moment it's seen,
				// baseline or not — this used to answer immediately and
				// must keep doing so.
				return finishChat()
			case "done", "failed":
				switch {
				case turnEndedAt > baselineTurnEndedAt:
					// The turn genuinely advanced past the baseline:
					// definitely this call's own answer.
					return finishChat()
				case seenInFlight:
					// A new turn was seen starting (or waiting) but hasn't
					// produced a fresh turn_ended_at yet — keep polling, no
					// time escape. Evidence of a real turn beats a clock.
				case time.Since(waitStart) >= waitResultIdleGrace:
					// In flight was NEVER observed, and the idle grace
					// window has elapsed: this is a child that was already
					// done/failed before the wait even started and never
					// budged — a genuinely idle child, not one whose new
					// turn just hasn't started producing output yet. The
					// window is wide (30s default) specifically so a normal
					// CLI cold start clears it by transitioning through
					// running/waiting first, landing in the branch above
					// instead of this one.
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

// waitResultIdleGrace bounds how long waitResult will hold a done/failed
// chat's answer back when the phase was ALREADY at that state and never once
// observed in flight (running/waiting_input/waiting_approval) during the
// wait — i.e. a child that looks like it was already idle before this wait
// even started, not one whose new turn just hasn't produced output yet. It
// is deliberately wide: a normal CLI cold start (queueing, a busy machine)
// can easily take several seconds to transition into running, and this
// window exists so that transition — not the clock — is what usually
// resolves the ambiguity; only a child that stays silent for this whole
// window is treated as genuinely idle. Once in flight HAS been observed,
// this grace does not apply at all — turn_ended_at must advance, however
// long that takes (bounded only by the caller's own timeout). A var, not a
// const, so a test can shrink it instead of sleeping for the real duration.
var waitResultIdleGrace = 30 * time.Second

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
