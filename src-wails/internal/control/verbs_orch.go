package control

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Orchestration state machine, ported from stablyai/orca's skill-guides/
// orchestration.md onto the one address space Burrow already has: target_key
// is "chat:<id>" or "pty:<id>", exactly phasestore.go's key shape — never a
// terminal handle. See docs/plans/orca-steal-2026-09-22.md section 4.
//
// Three entities: a Run is a durable namespace, a Task is the work, a
// Dispatch is ONE authoritative attempt at a Task. Separating Task from
// Dispatch is what makes retry legible — a second attempt is a new Dispatch
// of the same Task, never a new Task.
//
// Deliberately not here (see the plan): DAG dependencies, remote placement,
// depth limits, worker-abandon vs worker-stop, adopting foreign Runs.

// dispatchLiveness is the layered verdict orca's safety floor requires:
// `live` and `unverifiable` are read-only signals, `exited` is the only one
// positive proof (not absence) may produce.
type dispatchLiveness string

const (
	livenessLive         dispatchLiveness = "live"
	livenessUnverifiable dispatchLiveness = "unverifiable"
	livenessExited       dispatchLiveness = "exited"
)

// deriveLiveness maps a phase state plus a positive existence check onto
// liveness. exists=false is the ONLY thing that produces `exited` — it must
// come from a positive check (the daemon's own PTY list, or the chat row
// still being in the table), never from a timeout or a missing capability.
// `stale` is itself already an absence-derived inference (phasepoll.go's
// watchdog: three empty foreground reads), so it maps to `unverifiable`, not
// `exited` — absence never compounds into a stronger conclusion than the
// absence that produced it.
func deriveLiveness(phaseState string, exists bool) dispatchLiveness {
	if !exists {
		return livenessExited
	}
	if phaseState == "stale" {
		return livenessUnverifiable
	}
	return livenessLive
}

// settleOutcome validates and applies a worker_done outcome. It is the one
// place "never encode failure only in prose" is enforced: outcome must be
// exactly "succeeded" or "failed", and a Dispatch may be settled only once —
// a second worker_done for the same Dispatch is rejected, not merged.
func settleOutcome(currentOutcome, outcome string) (string, error) {
	if outcome != "succeeded" && outcome != "failed" {
		return "", fmt.Errorf("worker_done: outcome must be %q or %q, got %q", "succeeded", "failed", outcome)
	}
	if currentOutcome != "" {
		return "", fmt.Errorf("worker_done: dispatch already settled with outcome %q", currentOutcome)
	}
	return outcome, nil
}

// releaseAction is what a coordinator must do, exactly once, after an
// accepted outcome: reuse the terminal for an immediate follow-up Dispatch,
// retain it on user request, or release it back to the pool.
func validReleaseAction(action string) bool {
	return action == "reuse" || action == "retain" || action == "release"
}

// canRelease is orca's completion-accounting rule: release is post-settlement
// cleanup, and ONLY an accepted settlement authorizes it. A Dispatch with no
// outcome yet has nothing to release. A Dispatch whose liveness is
// `unverifiable` must not be released either — an absence-derived signal
// (the stale watchdog) must never be read as permission to free the slot,
// even if some other observation looked like a settlement.
func canRelease(outcome string, liveness dispatchLiveness) error {
	if outcome == "" {
		return fmt.Errorf("release: dispatch has no accepted outcome yet — absence never authorizes release")
	}
	if liveness == livenessUnverifiable {
		return fmt.Errorf("release: dispatch liveness is unverifiable — absence never authorizes release")
	}
	return nil
}

func orchVerbs(c *Core) []Verb {
	return []Verb{{
		Name:    "run_create",
		Summary: "Start a new orchestration Run — a durable namespace and coordinator inbox for a batch of Tasks",
		Args: []Arg{
			{Name: "objective", Type: "string", Desc: "What this Run is for", Required: true},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.runCreate(p) },
	}, {
		Name:    "task_dispatch",
		Summary: "Create a Task (or retry an existing one) and open its authoritative Dispatch against a chat or tab agent",
		Args: []Arg{
			// run_id is required to CREATE a Task, but not for a retry (which
			// supplies task_id instead) — that either/or is enforced in Fn,
			// not here, since the registry's Required check has no notion of
			// "one of these two".
			{Name: "run_id", Type: "integer", Desc: "Run this Task belongs to — required unless task_id retries an existing one"},
			{Name: "target_key", Type: "string", Desc: "chat:<id> or pty:<id> — the agent this Dispatch is attempted against", Required: true},
			{Name: "spec", Type: "string", Desc: "Self-contained task spec: target, change, constraints, ownership, observable acceptance"},
			{Name: "task_id", Type: "integer", Desc: "Retry: open a new Dispatch against this existing Task instead of creating one"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.taskDispatch(p) },
	}, {
		Name:    "worker_done",
		Summary: "Settle a Dispatch exactly once, with an explicit succeeded/failed outcome — never encode failure only in prose",
		Args: []Arg{
			{Name: "dispatch_id", Type: "integer", Desc: "Dispatch being settled", Required: true},
			{Name: "outcome", Type: "string", Desc: "succeeded or failed", Required: true},
			{Name: "summary", Type: "string", Desc: "Short executive summary of what happened"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.workerDone(p) },
	}, {
		Name:    "ask",
		Summary: "Block, as the dispatched worker, on a coordinator question until reply — never open a local question the coordinator cannot answer",
		Args: []Arg{
			{Name: "dispatch_id", Type: "integer", Desc: "The asking Dispatch", Required: true},
			{Name: "question", Type: "string", Desc: "The question for the coordinator", Required: true},
			{Name: "timeout", Type: "integer", Desc: "Seconds to wait (default 600)"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.ask(ctx, p) },
	}, {
		Name:    "reply",
		Summary: "Answer a pending ask — for a chat target the answer also flows over chat_send, for a tab over send_to_tab",
		Args: []Arg{
			{Name: "message_id", Type: "integer", Desc: "Message returned by a pending ask", Required: true},
			{Name: "body", Type: "string", Desc: "The answer", Required: true},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.reply(ctx, p) },
	}, {
		Name:    "worker_release",
		Summary: "Completion accounting: after an accepted outcome, record exactly one of reuse/retain/release for the Dispatch",
		Args: []Arg{
			{Name: "dispatch_id", Type: "integer", Desc: "Settled Dispatch", Required: true},
			{Name: "action", Type: "string", Desc: "reuse, retain, or release", Required: true},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.workerRelease(p) },
	}, {
		Name:    "reclaimable_dispatches",
		Summary: "List settled Dispatches that still owe a release decision — a coordinator turn must not end while this is non-empty",
		Args: []Arg{
			{Name: "run_id", Type: "integer", Desc: "Restrict to one Run"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.reclaimableDispatches(p) },
	}}
}

// OrchRun is the durable namespace a batch of Tasks belongs to.
type OrchRun struct {
	ID        int64  `json:"id"`
	Objective string `json:"objective"`
}

// OrchTask is the work; it outlives any single attempt at it.
type OrchTask struct {
	ID     int64  `json:"id"`
	RunID  int64  `json:"run_id"`
	Spec   string `json:"spec"`
	Status string `json:"status"`
}

// OrchDispatch is one authoritative attempt at a Task.
type OrchDispatch struct {
	ID           int64  `json:"id"`
	TaskID       int64  `json:"task_id"`
	TargetKey    string `json:"target_key"`
	Outcome      string `json:"outcome"`
	Summary      string `json:"summary,omitempty"`
	ReleaseState string `json:"release_state"`
	Liveness     string `json:"liveness"`
}

func (c *Core) runCreate(p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("run_create: no database")
	}
	res, err := c.deps.DB.Exec(`INSERT INTO orch_run (objective, created_at) VALUES (?, ?)`,
		p.Str("objective"), time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("run_create: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return OrchRun{ID: id, Objective: p.Str("objective")}, nil
}

func (c *Core) taskDispatch(p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("task_dispatch: no database")
	}
	targetKey := p.Str("target_key")
	if !strings.HasPrefix(targetKey, "chat:") && !strings.HasPrefix(targetKey, "pty:") {
		return nil, fmt.Errorf("task_dispatch: target_key must be chat:<id> or pty:<id>, got %q", targetKey)
	}

	taskID := p.Int("task_id")
	if taskID <= 0 {
		runID := p.Int("run_id")
		if runID <= 0 {
			return nil, fmt.Errorf("task_dispatch: needs run_id (new Task) or task_id (retry)")
		}
		var exists int
		if err := c.deps.DB.QueryRow(`SELECT 1 FROM orch_run WHERE id = ?`, runID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("task_dispatch: run %d not found", runID)
		}
		res, err := c.deps.DB.Exec(`INSERT INTO orch_task (run_id, spec, status, created_at) VALUES (?, ?, 'dispatched', ?)`,
			runID, p.Str("spec"), time.Now().UnixMilli())
		if err != nil {
			return nil, fmt.Errorf("task_dispatch: create task: %w", err)
		}
		taskID, err = res.LastInsertId()
		if err != nil {
			return nil, err
		}
	} else {
		// A retry: this Dispatch's Task already exists, no new Task is created.
		if _, err := c.deps.DB.Exec(`UPDATE orch_task SET status = 'dispatched' WHERE id = ?`, taskID); err != nil {
			return nil, fmt.Errorf("task_dispatch: retry task %d: %w", taskID, err)
		}
	}

	res, err := c.deps.DB.Exec(`INSERT INTO orch_dispatch (task_id, target_key, started_at) VALUES (?, ?, ?)`,
		taskID, targetKey, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("task_dispatch: create dispatch: %w", err)
	}
	dispatchID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return OrchDispatch{ID: dispatchID, TaskID: taskID, TargetKey: targetKey, Liveness: string(c.dispatchLiveness(targetKey))}, nil
}

func (c *Core) workerDone(p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("worker_done: no database")
	}
	dispatchID := p.Int("dispatch_id")
	var taskID int64
	var currentOutcome string
	if err := c.deps.DB.QueryRow(`SELECT task_id, outcome FROM orch_dispatch WHERE id = ?`, dispatchID).
		Scan(&taskID, &currentOutcome); err != nil {
		return nil, fmt.Errorf("worker_done: dispatch %d not found", dispatchID)
	}
	outcome, err := settleOutcome(currentOutcome, p.Str("outcome"))
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	if _, err := c.deps.DB.Exec(`UPDATE orch_dispatch SET outcome = ?, summary = ?, settled_at = ? WHERE id = ?`,
		outcome, p.Str("summary"), now, dispatchID); err != nil {
		return nil, fmt.Errorf("worker_done: %w", err)
	}
	if _, err := c.deps.DB.Exec(`UPDATE orch_task SET status = 'settled' WHERE id = ?`, taskID); err != nil {
		return nil, fmt.Errorf("worker_done: %w", err)
	}
	return map[string]any{"dispatch_id": dispatchID, "outcome": outcome}, nil
}

// ask blocks the dispatched worker on a coordinator question, the same
// poll-a-row shape waitResult already uses for chat completion — there is no
// separate notification channel to invent.
func (c *Core) ask(ctx context.Context, p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("ask: no database")
	}
	dispatchID := p.Int("dispatch_id")
	question := p.Str("question")
	res, err := c.deps.DB.Exec(`INSERT INTO orch_message (dispatch_id, body, created_at) VALUES (?, ?, ?)`,
		dispatchID, question, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("ask: %w", err)
	}
	messageID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(p.Int("timeout")) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		var status, replyBody string
		if err := c.deps.DB.QueryRow(`SELECT status, reply_body FROM orch_message WHERE id = ?`, messageID).
			Scan(&status, &replyBody); err != nil {
			return nil, fmt.Errorf("ask: %w", err)
		}
		if status == "replied" {
			return map[string]any{"message_id": messageID, "reply": replyBody}, nil
		}
		if time.Now().After(deadline) {
			// A timeout is a checkpoint, not a failure: the message stays
			// pending and resumable, it is not cancelled or deleted.
			return nil, fmt.Errorf("ask: message %d not answered within %s (still pending — resume with the same message_id)", messageID, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Core) reply(ctx context.Context, p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("reply: no database")
	}
	messageID := p.Int("message_id")
	res, err := c.deps.DB.Exec(`UPDATE orch_message SET status = 'replied', reply_body = ?, replied_at = ?
		WHERE id = ? AND status = 'pending'`,
		p.Str("body"), time.Now().UnixMilli(), messageID)
	if err != nil {
		return nil, fmt.Errorf("reply: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("reply: message %d not found or already answered", messageID)
	}
	return map[string]any{"replied": true}, nil
}

func (c *Core) workerRelease(p Params) (any, error) {
	if c.deps.DB == nil {
		return nil, fmt.Errorf("worker_release: no database")
	}
	action := p.Str("action")
	if !validReleaseAction(action) {
		return nil, fmt.Errorf("worker_release: action must be reuse, retain, or release, got %q", action)
	}
	dispatchID := p.Int("dispatch_id")
	var outcome, targetKey string
	if err := c.deps.DB.QueryRow(`SELECT outcome, target_key FROM orch_dispatch WHERE id = ?`, dispatchID).
		Scan(&outcome, &targetKey); err != nil {
		return nil, fmt.Errorf("worker_release: dispatch %d not found", dispatchID)
	}
	if err := canRelease(outcome, c.dispatchLiveness(targetKey)); err != nil {
		return nil, err
	}
	if _, err := c.deps.DB.Exec(`UPDATE orch_dispatch SET release_state = ? WHERE id = ?`, action, dispatchID); err != nil {
		return nil, fmt.Errorf("worker_release: %w", err)
	}
	return map[string]any{"dispatch_id": dispatchID, "release_state": action}, nil
}

func (c *Core) reclaimableDispatches(p Params) (any, error) {
	out := []OrchDispatch{}
	if c.deps.DB == nil {
		return out, nil
	}
	query := `SELECT id, task_id, target_key, outcome, summary FROM orch_dispatch
		WHERE outcome != '' AND release_state = ''`
	args := []any{}
	if runID := p.Int("run_id"); runID > 0 {
		query += ` AND task_id IN (SELECT id FROM orch_task WHERE run_id = ?)`
		args = append(args, runID)
	}
	rows, err := c.deps.DB.Query(query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d OrchDispatch
		if err := rows.Scan(&d.ID, &d.TaskID, &d.TargetKey, &d.Outcome, &d.Summary); err != nil {
			return out, err
		}
		d.Liveness = string(c.dispatchLiveness(d.TargetKey))
		out = append(out, d)
	}
	return out, rows.Err()
}

// dispatchLiveness combines the phase Go already derives with a positive
// existence check — never inventing a liveness source of its own. `exists`
// defaults true (no proof of exit) whenever a capability is missing, per the
// safety floor: absence of the capability must never masquerade as absence
// of the process.
func (c *Core) dispatchLiveness(targetKey string) dispatchLiveness {
	state := "idle"
	if c.deps.Phases != nil {
		state, _ = c.deps.Phases.Phase(targetKey)
	}
	return deriveLiveness(state, c.targetExists(targetKey))
}

func (c *Core) targetExists(targetKey string) bool {
	if id, ok := strings.CutPrefix(targetKey, "pty:"); ok {
		if c.deps.PTYs == nil {
			return true
		}
		ids, err := c.deps.PTYs.ListPtySessions()
		if err != nil {
			return true
		}
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	if id, ok := strings.CutPrefix(targetKey, "chat:"); ok {
		if c.deps.DB == nil {
			return true
		}
		var one int
		err := c.deps.DB.QueryRow(`SELECT 1 FROM chats WHERE id = ?`, id).Scan(&one)
		return err == nil
	}
	return true
}
