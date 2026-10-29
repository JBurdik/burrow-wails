package control

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeSchedules struct {
	chatID int64
	text   string
	due    time.Time
}

func (f *fakeSchedules) Add(chatID int64, text string, due time.Time) (ScheduledPrompt, error) {
	f.chatID, f.text, f.due = chatID, text, due
	return ScheduledPrompt{ID: 1, ChatID: chatID, Text: text, DueAt: due}, nil
}
func (f *fakeSchedules) List(int64) ([]ScheduledPrompt, error) { return nil, nil }
func (f *fakeSchedules) Cancel(int64) (bool, error)            { return true, nil }

func TestScheduleDefaultsToCallerChatAndParsesTimes(t *testing.T) {
	now := time.Date(2026, 10, 29, 17, 0, 0, 0, time.Local)
	defer func(old func() time.Time) { nowFunc = old }(nowFunc)
	nowFunc = func() time.Time { return now }

	f := &fakeSchedules{}
	c := newTestCore(t, Deps{Schedules: f})
	call := func(p Params) error {
		_, err := c.Call(context.Background(), ScopeLocal, "schedule", p)
		return err
	}

	if err := call(Params{"text": "Wake-up: CI", "in": "30m", "parent_chat_id": "7"}); err != nil {
		t.Fatal(err)
	}
	if f.chatID != 7 || !f.due.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("in: chat %d due %v", f.chatID, f.due)
	}
	if err := call(Params{"text": "x", "in": "2d", "chat_id": "9", "parent_chat_id": "7"}); err != nil || f.chatID != 9 || !f.due.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("explicit chat_id / days: %d %v %v", f.chatID, f.due, err)
	}
	// A clock time already past today means tomorrow.
	if err := call(Params{"text": "x", "at": "09:00", "parent_chat_id": "7"}); err != nil ||
		!f.due.Equal(time.Date(2026, 10, 30, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("at 09:00: %v %v", f.due, err)
	}
	if err := call(Params{"text": "x", "at": "2026-11-01T08:00:00Z", "parent_chat_id": "7"}); err != nil ||
		!f.due.Equal(time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("at RFC3339: %v %v", f.due, err)
	}

	for _, bad := range []Params{
		{"text": "x", "in": "30m"},                                       // no chat
		{"text": "x", "parent_chat_id": "7"},                             // neither in nor at
		{"text": "x", "in": "30m", "at": "09:00", "parent_chat_id": "7"}, // both
		{"text": "x", "in": "-5m", "parent_chat_id": "7"},
		{"text": "x", "at": "tomorrowish", "parent_chat_id": "7"},
	} {
		if err := call(bad); err == nil {
			t.Errorf("%v: want an error", bad)
		}
	}
}

type fakeMover struct {
	cwd   string
	moved int64
}

func (f *fakeMover) ChatCwd(int64) (string, error) { return f.cwd, nil }
func (f *fakeMover) MoveChat(_ int64, ws int64) error {
	f.moved = ws
	return nil
}

type fakeWorktrees struct{ created []string }

func (f *fakeWorktrees) Create(repoPath, name, path, branch, baseRef string) (int64, error) {
	f.created = append(f.created, fmt.Sprintf("%s %s %s", repoPath, path, branch))
	return 42, nil
}
func (f *fakeWorktrees) Remove(int64, bool) error { return nil }

// An existing worktree of the repo on that branch is reused; a new branch gets
// one created; leave_worktree climbs back to the parent repo.
func TestMoveToWorktreeAndLeave(t *testing.T) {
	db := openTestDB(t)
	mover := &fakeMover{cwd: "/tmp/repo"}
	wts := &fakeWorktrees{}
	c := newTestCore(t, Deps{DB: db, ChatMover: mover, Worktrees: wts, WorktreesDir: func() string { return "/wt" }})
	ctx := context.Background()

	out, err := c.Call(ctx, ScopeLocal, "move_to_worktree", Params{"branch": "feat/x", "parent_chat_id": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if mover.moved != 2 || len(wts.created) != 0 || out.(map[string]any)["path"] != "/tmp/wt/feat-x" {
		t.Fatalf("reuse: moved=%d created=%v out=%v", mover.moved, wts.created, out)
	}

	if _, err := c.Call(ctx, ScopeLocal, "move_to_worktree", Params{"branch": "feat/y", "parent_chat_id": "3"}); err != nil {
		t.Fatal(err)
	}
	if mover.moved != 42 || len(wts.created) != 1 || wts.created[0] != "/tmp/repo /wt/repo/feat/y feat/y" {
		t.Fatalf("create: moved=%d created=%v", mover.moved, wts.created)
	}

	mover.cwd = "/tmp/wt/feat-x"
	if _, err := c.Call(ctx, ScopeLocal, "leave_worktree", Params{"parent_chat_id": "3"}); err != nil || mover.moved != 1 {
		t.Fatalf("leave: moved=%d err=%v", mover.moved, err)
	}
	mover.cwd = "/tmp/repo"
	if _, err := c.Call(ctx, ScopeLocal, "leave_worktree", Params{"parent_chat_id": "3"}); err == nil || !strings.Contains(err.Error(), "not in a worktree") {
		t.Fatalf("leave from the repo itself: %v", err)
	}
	if _, err := c.Call(ctx, ScopeLocal, "move_to_worktree", Params{"branch": "feat/x"}); err == nil {
		t.Fatal("a call with no chat moved something")
	}
}
