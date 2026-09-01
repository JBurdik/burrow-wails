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
		Name:    "wait_result",
		Summary: "Block until a spawned agent finishes and return its final message",
		Args: []Arg{
			{Name: "token", Type: "string", Desc: "Token returned by spawn", Required: true},
			{Name: "timeout", Type: "integer", Desc: "Seconds to wait (default 600)"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.waitResult(ctx, p) },
	}, {
		Name:    "collect_results",
		Summary: "Take every finished sub-agent result that hasn't been collected yet",
		Scope:   ScopeLocal,
		Fn:      func(ctx context.Context, p Params) (any, error) { return c.collectResults() },
	}}
}

func (c *Core) spawn(ctx context.Context, p Params) (any, error) {
	target := p.Str("target")
	if target == "" {
		target = "tab"
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
		"task":   p.Str("task"),
		"agent":  p.Str("agent"),
		"model":  p.Str("model"),
		"cwd":    p.Str("cwd"),
		"target": target,
		"token":  token,
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

// waitResult polls for the capture file. Polling (not inotify) because the
// writer is a hook in another process and the wait is measured in minutes —
// a 500ms tick is free at that scale and has no watcher lifecycle to leak.
func (c *Core) waitResult(ctx context.Context, p Params) (any, error) {
	token := p.Str("token")
	timeout := time.Duration(p.Int("timeout")) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)

	for {
		if text, ok := c.takeResult(token); ok {
			return Result{Token: token, Text: text}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("wait_result: %s did not finish within %s (it may still be working — check agent_status)", token, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Core) collectResults() (any, error) {
	if c.deps.SessionDir == "" {
		return []Result{}, nil
	}
	entries, err := os.ReadDir(c.deps.SessionDir)
	if err != nil {
		return []Result{}, nil // no session dir yet = nothing has ever been spawned
	}
	tokens := []string{}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".done") {
			tokens = append(tokens, strings.TrimSuffix(name, ".done"))
		}
	}
	sort.Strings(tokens) // tokens are time-ordered, so this is chronological

	out := []Result{}
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
