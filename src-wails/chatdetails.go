package main

import (
	"context"
	"fmt"

	"burrow/internal/control"
)

// The thread details card's door to the self-management verbs: the same
// registry calls `burrow schedule_list` / `move_to_worktree` make, scoped to
// one chat the desktop names (an agent's own calls default to its chat).

func (a *App) selfVerb(verb string, params control.Params) (any, error) {
	if a.control == nil {
		return nil, fmt.Errorf("%s: control surface not ready", verb)
	}
	return a.control.Call(context.Background(), control.ScopeLocal, verb, params)
}

// ChatScheduleList is one chat's pending scheduled prompts. chatID must be set
// (0 would list every chat's).
func (a *App) ChatScheduleList(chatID int64) ([]control.ScheduledPrompt, error) {
	if chatID <= 0 {
		return []control.ScheduledPrompt{}, nil
	}
	return scheduleAdapter{a}.List(chatID)
}

func (a *App) ChatScheduleCancel(id int64) (bool, error) {
	return scheduleAdapter{a}.Cancel(id)
}

// Settings → Scheduled: every chat's items, create / edit (id 0 = new),
// pause / resume, run now. Delete is ChatScheduleCancel.

func (a *App) ScheduleListAll() ([]control.ScheduledPrompt, error) {
	return scheduleAdapter{a}.List(0)
}

func (a *App) ScheduleSave(p control.ScheduledPrompt) (control.ScheduledPrompt, error) {
	return scheduleAdapter{a}.save(p)
}

func (a *App) ScheduleSetPaused(id int64, paused bool) (control.ScheduledPrompt, error) {
	return scheduleAdapter{a}.SetPaused(id, paused)
}

func (a *App) ScheduleRunNow(id int64) (control.ScheduledPrompt, error) {
	return scheduleAdapter{a}.RunNow(id)
}

func (a *App) ChatMoveToWorktree(chatID int64, branch string) (any, error) {
	return a.selfVerb("move_to_worktree", control.Params{"chat_id": chatID, "branch": branch})
}

func (a *App) ChatLeaveWorktree(chatID int64) (any, error) {
	return a.selfVerb("leave_worktree", control.Params{"chat_id": chatID})
}
