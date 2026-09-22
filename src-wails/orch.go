package main

// Schema for the orchestration state machine (internal/control's orch verbs).
// Three entities, matching skill-guides/orchestration.md from stablyai/orca:
//
//   - Run    a durable namespace and coordinator inbox (orch_run).
//   - Task   the work (orch_task).
//   - Dispatch  ONE authoritative attempt at a Task (orch_dispatch). A retry is
//     a new Dispatch of the same Task, never a new Task — that is what keeps a
//     second attempt legible instead of indistinguishable from the first.
//
// orch_dispatch.target_key is "chat:<id>" or "pty:<id>" — the exact key shape
// phasestore.go already uses, so liveness needs no second derivation.
// orch_message backs the blocking ask/reply verbs: a worker's `ask` blocks
// polling this row until the coordinator's `reply` fills it in, the same
// poll-a-row shape wait_result already uses for chat completion.
func orchSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS orch_run (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			objective  TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS orch_task (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id     INTEGER NOT NULL REFERENCES orch_run(id),
			spec       TEXT    NOT NULL,
			status     TEXT    NOT NULL DEFAULT 'pending', -- pending | dispatched | settled
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS orch_task_run ON orch_task(run_id)`,
		`CREATE TABLE IF NOT EXISTS orch_dispatch (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id       INTEGER NOT NULL REFERENCES orch_task(id),
			target_key    TEXT    NOT NULL, -- "chat:<id>" or "pty:<id>" — never a terminal handle
			outcome       TEXT    NOT NULL DEFAULT '', -- '' | succeeded | failed
			summary       TEXT    NOT NULL DEFAULT '',
			release_state TEXT    NOT NULL DEFAULT '', -- '' | reused | retained | released
			started_at    INTEGER NOT NULL,
			settled_at    INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS orch_dispatch_task ON orch_dispatch(task_id)`,
		`CREATE TABLE IF NOT EXISTS orch_message (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			dispatch_id INTEGER NOT NULL REFERENCES orch_dispatch(id),
			body        TEXT    NOT NULL,
			status      TEXT    NOT NULL DEFAULT 'pending', -- pending | replied
			reply_body  TEXT    NOT NULL DEFAULT '',
			created_at  INTEGER NOT NULL,
			replied_at  INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS orch_message_dispatch ON orch_message(dispatch_id)`,
	}
}
