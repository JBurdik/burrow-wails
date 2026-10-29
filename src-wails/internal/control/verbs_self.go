package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Self-management verbs: what an agent in a chat does to ITSELF — prompt
// itself later, move its own thread into a worktree and back. Each defaults to
// the caller's chat (parent_chat_id, injected from BURROW_CHAT_ID by the CLI and
// burrow-mcp), so an agent never has to know its own id.
//
// Local-only: a scheduled prompt is a deferred chat_send and a move restarts the
// agent somewhere else — both drive an agent that holds tool permissions, which
// is exactly what ScopeRemote verbs are kept away from.

// ScheduledPrompt is one pending scheduled message.
type ScheduledPrompt struct {
	ID        int64     `json:"id"`
	ChatID    int64     `json:"chat_id"`
	Text      string    `json:"text"`
	DueAt     time.Time `json:"due_at"`
	CreatedAt time.Time `json:"created_at"`
	// Attempts/LastError: a delivery that failed (the agent would not start)
	// stays queued and is retried by the sweep, a few times.
	Attempts  int    `json:"attempts,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// Schedules persists scheduled prompts; the host's sweep delivers them through
// the same door as chat_send once due and the chat's turn has settled.
type Schedules interface {
	Add(chatID int64, text string, due time.Time) (ScheduledPrompt, error)
	// List returns pending items, soonest first; chatID 0 = every chat.
	List(chatID int64) ([]ScheduledPrompt, error)
	Cancel(id int64) (bool, error)
}

// ChatMover re-homes a chat in another workspace: its next turn starts there,
// with the conversation intact.
type ChatMover interface {
	ChatCwd(chatID int64) (string, error)
	MoveChat(chatID, workspaceID int64) error
}

// nowFunc is the clock schedule parses `in`/`at` against; a var for tests.
var nowFunc = time.Now

func selfVerbs(c *Core) []Verb {
	chatArg := Arg{Name: "chat_id", Type: "integer", Desc: "Chat to act on; defaults to the calling chat (BURROW_CHAT_ID)"}
	return []Verb{{
		Name:    "schedule",
		Summary: "Schedule a prompt to be sent to a chat later — a reminder or wake-up for yourself. Survives restarts; waits for a running turn to finish",
		Args: []Arg{
			{Name: "text", Type: "string", Desc: "The prompt to send when due, e.g. \"Wake-up: check whether CI on #42 passed\"", Required: true},
			{Name: "in", Type: "string", Desc: "Delay from now: 90s, 30m, 2h, 1h30m, 1d (pass in or at)"},
			{Name: "at", Type: "string", Desc: "Absolute time: RFC3339, \"2006-01-02 15:04\" or \"15:04\" local (next occurrence)"},
			chatArg,
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.schedule(p) },
	}, {
		Name:    "schedule_list",
		Summary: "Pending scheduled prompts, soonest first",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Only this chat's; defaults to the calling chat"},
			{Name: "all", Type: "boolean", Desc: "Every chat's, ignoring chat_id"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			if c.deps.Schedules == nil {
				return nil, fmt.Errorf("schedule_list: no scheduler")
			}
			chatID := callerChat(p)
			if p.Bool("all") {
				chatID = 0
			}
			return c.deps.Schedules.List(chatID)
		},
	}, {
		Name:    "schedule_cancel",
		Summary: "Cancel a pending scheduled prompt",
		Args:    []Arg{{Name: "id", Type: "integer", Desc: "Schedule id from schedule / schedule_list", Required: true}},
		Scope:   ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			if c.deps.Schedules == nil {
				return nil, fmt.Errorf("schedule_cancel: no scheduler")
			}
			ok, err := c.deps.Schedules.Cancel(p.Int("id"))
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("schedule_cancel: no pending schedule %d", p.Int("id"))
			}
			return map[string]any{"cancelled": p.Int("id")}, nil
		},
	}, {
		Name:    "move_to_worktree",
		Summary: "Move this chat into a git worktree of its repo (created if missing); the next turn runs there with the conversation intact",
		Args: []Arg{
			{Name: "branch", Type: "string", Desc: "Worktree branch; an existing worktree of this repo on that branch is reused, else one is created", Required: true},
			{Name: "base", Type: "string", Desc: "Base ref for a new branch (default HEAD)"},
			{Name: "path", Type: "string", Desc: "Override the default <worktreesDir>/<repo>/<branch> location of a new worktree"},
			chatArg,
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.moveToWorktree(p) },
	}, {
		Name:    "leave_worktree",
		Summary: "Move this chat from its worktree back to the parent repo; the worktree itself is kept (worktree_remove deletes it)",
		Args:    []Arg{chatArg},
		Scope:   ScopeLocal,
		Fn:      func(ctx context.Context, p Params) (any, error) { return c.leaveWorktree(p) },
	}}
}

// callerChat is the explicit chat_id, else the calling chat.
func callerChat(p Params) int64 {
	if id := p.Int("chat_id"); id > 0 {
		return id
	}
	return p.Int("parent_chat_id")
}

func (c *Core) schedule(p Params) (any, error) {
	if c.deps.Schedules == nil {
		return nil, fmt.Errorf("schedule: no scheduler")
	}
	chatID := callerChat(p)
	if chatID <= 0 {
		return nil, fmt.Errorf("schedule: no chat — pass chat_id (only chats can be scheduled, not terminal tabs)")
	}
	text := strings.TrimSpace(p.Str("text"))
	if text == "" {
		return nil, fmt.Errorf("schedule needs text")
	}
	in, at := p.Str("in"), p.Str("at")
	if (in == "") == (at == "") {
		return nil, fmt.Errorf("schedule: pass exactly one of in or at")
	}
	now := nowFunc()
	var due time.Time
	var err error
	if in != "" {
		var d time.Duration
		if d, err = parseDelay(in); err == nil {
			due = now.Add(d)
		}
	} else {
		due, err = parseAt(at, now)
	}
	if err != nil {
		return nil, fmt.Errorf("schedule: %w", err)
	}
	return c.deps.Schedules.Add(chatID, text, due)
}

// parseDelay is time.ParseDuration plus whole days ("1d", "2d").
func parseDelay(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	var d time.Duration
	var err error
	if n, ok := strings.CutSuffix(s, "d"); ok {
		var days int
		if days, err = strconv.Atoi(n); err == nil {
			d = time.Duration(days) * 24 * time.Hour
		}
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("bad delay %q (use e.g. 30m, 2h, 1d)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("delay must be positive, got %q", s)
	}
	return d, nil
}

// parseAt accepts RFC3339, a local date+time, or a local clock time — the next
// occurrence of it, so "09:00" said at 17:00 means tomorrow morning.
func parseAt(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("15:04", s, now.Location()); err == nil {
		due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !due.After(now) {
			due = due.AddDate(0, 0, 1)
		}
		return due, nil
	}
	return time.Time{}, fmt.Errorf("bad time %q (use RFC3339, \"2006-01-02 15:04\" or \"15:04\")", s)
}

// moveNote tells the calling agent what a move means for the turn it is in.
const moveNote = "Takes effect from the chat's next turn: this turn keeps running in the old directory, so work under the new path (absolute paths) for the rest of it. The next prompt starts there with the conversation intact."

func (c *Core) moveToWorktree(p Params) (any, error) {
	if c.deps.ChatMover == nil || c.deps.Worktrees == nil {
		return nil, fmt.Errorf("move_to_worktree: not available")
	}
	chatID := callerChat(p)
	if chatID <= 0 {
		return nil, fmt.Errorf("move_to_worktree: no chat — only a chat can move (pass chat_id)")
	}
	branch := p.Str("branch")
	cwd, err := c.deps.ChatMover.ChatCwd(chatID)
	if err != nil {
		return nil, fmt.Errorf("move_to_worktree: %w", err)
	}
	repoID, repoPath, err := c.repoOf(cwd)
	if err != nil {
		return nil, fmt.Errorf("move_to_worktree: %w", err)
	}

	var wsID int64
	var path string
	created := false
	err = c.deps.DB.QueryRow(`SELECT id, path FROM workspaces WHERE parent_id = ? AND worktree_branch = ?`, repoID, branch).
		Scan(&wsID, &path)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		path = c.worktreePath(repoPath, branch, p.Str("path"))
		if wsID, err = c.deps.Worktrees.Create(repoPath, branch, path, branch, p.Str("base")); err != nil {
			return nil, fmt.Errorf("move_to_worktree: %w", err)
		}
		created = true
	case err != nil:
		return nil, fmt.Errorf("move_to_worktree: %w", err)
	}
	if path == cwd {
		return map[string]any{"chat_id": chatID, "workspace_id": wsID, "path": path, "branch": branch, "moved": false}, nil
	}
	if err := c.deps.ChatMover.MoveChat(chatID, wsID); err != nil {
		return nil, fmt.Errorf("move_to_worktree: %w", err)
	}
	return map[string]any{"chat_id": chatID, "workspace_id": wsID, "path": path, "branch": branch,
		"created": created, "moved": true, "note": moveNote}, nil
}

func (c *Core) leaveWorktree(p Params) (any, error) {
	if c.deps.ChatMover == nil {
		return nil, fmt.Errorf("leave_worktree: not available")
	}
	chatID := callerChat(p)
	if chatID <= 0 {
		return nil, fmt.Errorf("leave_worktree: no chat — pass chat_id")
	}
	cwd, err := c.deps.ChatMover.ChatCwd(chatID)
	if err != nil {
		return nil, fmt.Errorf("leave_worktree: %w", err)
	}
	var parent sql.NullInt64
	if err := c.deps.DB.QueryRow(`SELECT parent_id FROM workspaces WHERE path = ?`, cwd).Scan(&parent); err != nil {
		return nil, fmt.Errorf("leave_worktree: %s is not a workspace", cwd)
	}
	if !parent.Valid {
		return nil, fmt.Errorf("leave_worktree: chat %d is not in a worktree", chatID)
	}
	var path string
	if err := c.deps.DB.QueryRow(`SELECT path FROM workspaces WHERE id = ?`, parent.Int64).Scan(&path); err != nil {
		return nil, fmt.Errorf("leave_worktree: parent workspace %d is gone", parent.Int64)
	}
	if err := c.deps.ChatMover.MoveChat(chatID, parent.Int64); err != nil {
		return nil, fmt.Errorf("leave_worktree: %w", err)
	}
	return map[string]any{"chat_id": chatID, "workspace_id": parent.Int64, "path": path, "left": cwd, "note": moveNote}, nil
}
