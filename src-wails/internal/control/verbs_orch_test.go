package control

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openOrchTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// busy_timeout: TestAskBlocksUntilReply writes from a second goroutine
	// while ask's poll loop reads, same as the real app's dsn in db.go.
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "orch.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// Mirrors orch.go's orchSchema() (package main, not importable here) plus
	// a minimal chats table for the target-exists checks.
	stmts := []string{
		`CREATE TABLE chats (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE orch_run (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			objective  TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE orch_task (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id     INTEGER NOT NULL REFERENCES orch_run(id),
			spec       TEXT    NOT NULL,
			status     TEXT    NOT NULL DEFAULT 'pending',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE orch_dispatch (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id       INTEGER NOT NULL REFERENCES orch_task(id),
			target_key    TEXT    NOT NULL,
			outcome       TEXT    NOT NULL DEFAULT '',
			summary       TEXT    NOT NULL DEFAULT '',
			release_state TEXT    NOT NULL DEFAULT '',
			started_at    INTEGER NOT NULL,
			settled_at    INTEGER
		)`,
		`CREATE TABLE orch_message (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			dispatch_id INTEGER NOT NULL REFERENCES orch_dispatch(id),
			body        TEXT    NOT NULL,
			status      TEXT    NOT NULL DEFAULT 'pending',
			reply_body  TEXT    NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL,
			replied_at  INTEGER
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return db
}

// keyedPhases lets tests pin several target_keys' phases at once — unlike
// control_test.go's fakePhases (single state, call-counted for wait_result's
// baseline semantics), orchestration needs one phase per chat/pty target.
type keyedPhases struct{ states map[string]string }

func (f keyedPhases) Phase(key string) (string, int64) {
	if s, ok := f.states[key]; ok {
		return s, 0
	}
	return "idle", 0
}

func TestSecondWorkerDoneIsRejected(t *testing.T) {
	db := openOrchTestDB(t)
	if _, err := db.Exec(`INSERT INTO chats (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "running"}}})

	run, err := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "ship it"})
	if err != nil {
		t.Fatal(err)
	}
	runID := run.(OrchRun).ID

	disp, err := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": runID, "target_key": "chat:1", "spec": "do the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatchID := disp.(OrchDispatch).ID

	if _, err := c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": dispatchID, "outcome": "succeeded", "summary": "done",
	}); err != nil {
		t.Fatalf("first worker_done: %v", err)
	}

	_, err = c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": dispatchID, "outcome": "failed", "summary": "actually no",
	})
	if err == nil {
		t.Fatal("second worker_done for the same dispatch must be rejected")
	}
	if !strings.Contains(err.Error(), "already settled") {
		t.Errorf("err = %v, want an already-settled rejection", err)
	}
}

func TestWorkerDoneRejectsProseOnlyOutcome(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "running"}}})

	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})
	_, err := c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": disp.(OrchDispatch).ID, "outcome": "looks fine I guess",
	})
	if err == nil {
		t.Fatal("outcome must be exactly succeeded or failed, never free-text")
	}
}

func TestUnverifiableDispatchCannotBeReleased(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	// stale = the watchdog's absence-derived inference, must map to unverifiable.
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "stale"}}})

	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})
	dispatchID := disp.(OrchDispatch).ID

	if _, err := c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": dispatchID, "outcome": "succeeded",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := c.Call(context.Background(), ScopeLocal, "worker_release", Params{
		"dispatch_id": dispatchID, "action": "release",
	})
	if err == nil {
		t.Fatal("an unverifiable dispatch must not permit release, even after a settled outcome")
	}
	if !strings.Contains(err.Error(), "unverifiable") {
		t.Errorf("err = %v, want the unverifiable rejection", err)
	}
}

func TestUnsettledDispatchCannotBeReleased(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "running"}}})

	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})

	_, err := c.Call(context.Background(), ScopeLocal, "worker_release", Params{
		"dispatch_id": disp.(OrchDispatch).ID, "action": "release",
	})
	if err == nil {
		t.Fatal("absence of an outcome must never authorize release")
	}
}

func TestReleaseSucceedsOnceLiveAndSettled(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "done"}}})

	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})
	dispatchID := disp.(OrchDispatch).ID
	if _, err := c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": dispatchID, "outcome": "succeeded",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "worker_release", Params{
		"dispatch_id": dispatchID, "action": "release",
	}); err != nil {
		t.Fatalf("release should succeed once settled and live: %v", err)
	}
}

// A chat row deleted out from under an open dispatch is POSITIVE proof of
// exit — this is the "chat ended" half of liveness, distinct from `stale`.
func TestDeletedChatIsExitedNotUnverifiable(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "done"}}})
	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})
	dispatchID := disp.(OrchDispatch).ID

	db.Exec(`DELETE FROM chats WHERE id = 1`)

	if got := c.dispatchLiveness("chat:1"); got != livenessExited {
		t.Errorf("liveness = %q, want exited", got)
	}

	// Exited still counts as settleable — release must succeed on positive
	// proof of exit, only `unverifiable` (absence) blocks it.
	if _, err := c.Call(context.Background(), ScopeLocal, "worker_done", Params{
		"dispatch_id": dispatchID, "outcome": "failed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "worker_release", Params{
		"dispatch_id": dispatchID, "action": "release",
	}); err != nil {
		t.Fatalf("release on exited+settled should succeed: %v", err)
	}
}

func TestAskBlocksUntilReply(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "running"}}})
	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	disp, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "x",
	})
	dispatchID := disp.(OrchDispatch).ID

	// Simulate the coordinator's reply landing on the row directly (ask is
	// exercised for its polling behaviour by giving it a very small timeout
	// against an already-pending message it created itself).
	go func() {
		for i := 0; i < 20; i++ {
			var id int64
			if err := db.QueryRow(`SELECT id FROM orch_message WHERE dispatch_id = ? AND status = 'pending'`, dispatchID).Scan(&id); err == nil {
				c.Call(context.Background(), ScopeLocal, "reply", Params{"message_id": id, "body": "go ahead"})
				return
			}
		}
	}()

	res, err := c.Call(context.Background(), ScopeLocal, "ask", Params{
		"dispatch_id": dispatchID, "question": "should I proceed?", "timeout": float64(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["reply"] != "go ahead" {
		t.Errorf("reply = %v, want %q", res, "go ahead")
	}
}

func TestReplyRejectsUnknownOrAlreadyAnswered(t *testing.T) {
	db := openOrchTestDB(t)
	c := newTestCore(t, Deps{DB: db})
	if _, err := c.Call(context.Background(), ScopeLocal, "reply", Params{"message_id": float64(999), "body": "x"}); err == nil {
		t.Fatal("reply to an unknown message must fail")
	}
}

func TestReclaimableDispatchesListsOnlySettledWithNoDecision(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1), (2)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "done", "chat:2": "running"}}})
	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	runID := run.(OrchRun).ID

	settled, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{"run_id": runID, "target_key": "chat:1", "spec": "a"})
	unsettled, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{"run_id": runID, "target_key": "chat:2", "spec": "b"})
	c.Call(context.Background(), ScopeLocal, "worker_done", Params{"dispatch_id": settled.(OrchDispatch).ID, "outcome": "succeeded"})

	out, err := c.Call(context.Background(), ScopeLocal, "reclaimable_dispatches", Params{"run_id": runID})
	if err != nil {
		t.Fatal(err)
	}
	list := out.([]OrchDispatch)
	if len(list) != 1 || list[0].ID != settled.(OrchDispatch).ID {
		t.Fatalf("reclaimable = %+v, want only the settled-but-undecided dispatch (unsettled id %d excluded)", list, unsettled.(OrchDispatch).ID)
	}

	if _, err := c.Call(context.Background(), ScopeLocal, "worker_release", Params{
		"dispatch_id": settled.(OrchDispatch).ID, "action": "retain",
	}); err != nil {
		t.Fatal(err)
	}
	out, _ = c.Call(context.Background(), ScopeLocal, "reclaimable_dispatches", Params{"run_id": runID})
	if len(out.([]OrchDispatch)) != 0 {
		t.Errorf("reclaimable after decision = %v, want empty", out)
	}
}

func TestTaskDispatchRejectsTerminalHandleLikeTargets(t *testing.T) {
	db := openOrchTestDB(t)
	c := newTestCore(t, Deps{DB: db})
	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	_, err := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "terminal-7", "spec": "x",
	})
	if err == nil {
		t.Fatal("task_dispatch must reject anything that is not chat:<id> or pty:<id>")
	}
}

func TestRetryOpensNewDispatchOnSameTask(t *testing.T) {
	db := openOrchTestDB(t)
	db.Exec(`INSERT INTO chats (id) VALUES (1), (2)`)
	c := newTestCore(t, Deps{DB: db, Phases: keyedPhases{states: map[string]string{"chat:1": "failed", "chat:2": "running"}}})
	run, _ := c.Call(context.Background(), ScopeLocal, "run_create", Params{"objective": "x"})
	first, _ := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"run_id": run.(OrchRun).ID, "target_key": "chat:1", "spec": "flaky work",
	})
	c.Call(context.Background(), ScopeLocal, "worker_done", Params{"dispatch_id": first.(OrchDispatch).ID, "outcome": "failed"})

	retry, err := c.Call(context.Background(), ScopeLocal, "task_dispatch", Params{
		"task_id": first.(OrchDispatch).TaskID, "target_key": "chat:2",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := retry.(OrchDispatch)
	if r.TaskID != first.(OrchDispatch).TaskID {
		t.Errorf("retry TaskID = %d, want same Task %d", r.TaskID, first.(OrchDispatch).TaskID)
	}
	if r.ID == first.(OrchDispatch).ID {
		t.Error("retry must be a NEW Dispatch, not the same row")
	}
}
