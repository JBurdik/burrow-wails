package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"burrow/internal/agentphase"
)

// The exact pty_phase this machine had on disk: the key column still named
// pty_id, and no seq at all. Written by a build from before those changes, and
// left alone forever after by CREATE TABLE IF NOT EXISTS.
const legacyPtyPhaseSchema = `CREATE TABLE pty_phase (
	pty_id        TEXT PRIMARY KEY,
	state         TEXT NOT NULL,
	detail        TEXT NOT NULL DEFAULT '',
	model         TEXT NOT NULL DEFAULT '',
	title         TEXT NOT NULL DEFAULT '',
	is_agent      INTEGER NOT NULL DEFAULT 0,
	turn_ended_at INTEGER NOT NULL DEFAULT 0,
	updated_at    INTEGER NOT NULL DEFAULT 0
)`

// openLegacyDB creates the pre-rename table FIRST, then runs the real
// migration over it — which is the order a returning user's install is in.
func openLegacyDB(t *testing.T, seedRow bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(legacyPtyPhaseSchema); err != nil {
		t.Fatal(err)
	}
	if seedRow {
		if _, err := db.Exec(
			`INSERT INTO pty_phase (pty_id, state, title, is_agent, turn_ended_at, updated_at)
			 VALUES ('pty:7', 'running', 'old task', 1, 0, 123)`); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate over the legacy table failed: %v", err)
	}
	return db
}

func TestMigrateRenamesThePtyPhaseKeyColumn(t *testing.T) {
	db := openLegacyDB(t, false)

	cols, err := tableColumns(db, "pty_phase")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["id"] {
		t.Fatal("pty_phase has no `id` column after migrate")
	}
	if cols["pty_id"] {
		t.Fatal("pty_phase still has the old `pty_id` column")
	}
	if !cols["seq"] {
		t.Fatal("pty_phase has no `seq` column after migrate")
	}
}

func TestMigratePreservesExistingPhaseRows(t *testing.T) {
	// A rename, not a rebuild: whatever the last session recorded has to
	// survive, or every open terminal comes back with no phase.
	db := openLegacyDB(t, true)

	var id, state, title string
	if err := db.QueryRow(`SELECT id, state, title FROM pty_phase`).Scan(&id, &state, &title); err != nil {
		t.Fatal(err)
	}
	if id != "pty:7" || state != "running" || title != "old task" {
		t.Fatalf("row lost in migration: %q %q %q", id, state, title)
	}
	var seq int64
	if err := db.QueryRow(`SELECT seq FROM pty_phase`).Scan(&seq); err != nil {
		t.Fatalf("seq unreadable on a migrated row: %v", err)
	}
	if seq != 0 {
		t.Fatalf("migrated row got seq %d, want the 0 default", seq)
	}
}

// TestPhaseStoreOpensOnAMigratedLegacyDB is the one that matters: the symptom
// was never a SQL error the user saw, it was a nil PhaseStore and dead status
// dots everywhere.
func TestPhaseStoreOpensOnAMigratedLegacyDB(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	db := openLegacyDB(t, true)

	store, err := NewPhaseStore(db)
	if err != nil {
		t.Fatalf("phase store still fails on a migrated legacy db: %v", err)
	}
	store.Apply("pty:9", agentphase.Event{Kind: agentphase.HookRunning})
	if got := store.Get("pty:9").State; got != agentphase.Running {
		t.Fatalf("phase not applied: %v", got)
	}
}

func TestMigrateIsANoOpOnAFreshDB(t *testing.T) {
	// The repair must not fire on an install that never had the old table,
	// and must be safe to run twice — migrate() runs on every launch.
	db, err := openDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("second migrate on a fresh db failed: %v", err)
	}
	cols, err := tableColumns(db, "pty_phase")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["id"] || !cols["seq"] || cols["pty_id"] {
		t.Fatalf("fresh schema wrong after a repeat migrate: %v", cols)
	}
}

func TestTableColumnsOnAMissingTable(t *testing.T) {
	// pragma_table_info yields no rows rather than an error, and the repair
	// leans on that to tell "no table yet" from "table with wrong columns".
	db, err := openDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cols, err := tableColumns(db, "no_such_table")
	if err != nil {
		t.Fatalf("missing table should not error: %v", err)
	}
	if len(cols) != 0 {
		t.Fatalf("want no columns, got %v", cols)
	}
}

// legacyChatsSchema is the `chats` table shape from before parent_chat_id and
// collected_at existed (thread sub-agents). CREATE TABLE IF NOT EXISTS never
// migrates an existing table, so every install created before that feature
// keeps this shape until the `alters` block in migrate() adds the columns.
const legacyChatsSchema = `CREATE TABLE chats (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	workspace_id      INTEGER NOT NULL,
	title             TEXT    NOT NULL DEFAULT '',
	pinned_title      INTEGER NOT NULL DEFAULT 0,
	claude_session_id TEXT    NOT NULL DEFAULT '',
	message_count     INTEGER NOT NULL DEFAULT 0,
	control           INTEGER NOT NULL DEFAULT 0,
	agent_kind        TEXT    NOT NULL DEFAULT '',
	transport         TEXT    NOT NULL DEFAULT '',
	model             TEXT    NOT NULL DEFAULT '',
	branch            TEXT    NOT NULL DEFAULT '',
	settled_override  TEXT    NOT NULL DEFAULT '',
	archived_at       INTEGER NOT NULL DEFAULT 0,
	last_activity_at  INTEGER NOT NULL DEFAULT 0
)`

// TestMigrateUpgradesAPreThreadSubAgentsChatsTable reproduces the CRITICAL
// upgrade failure: chatsSchema()'s `chats_parent` index used to be created in
// the same statement block as the CREATE TABLE, which runs BEFORE the
// `alters` block that adds parent_chat_id. On a fresh DB the CREATE TABLE
// already carries the column, so the bug was invisible; on an upgraded DB
// (this test's setup) the table pre-dates it, CREATE TABLE IF NOT EXISTS is a
// no-op, and the index statement failed with "no such column:
// parent_chat_id" — which migrate() propagated as an error, leaving a.db nil
// and the app with no workspaces, chats or tabs.
func TestMigrateUpgradesAPreThreadSubAgentsChatsTable(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy_chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(legacyChatsSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO chats (workspace_id, title) VALUES (1, 'old chat')`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate over a pre-thread-sub-agents chats table failed: %v", err)
	}

	cols, err := tableColumns(db, "chats")
	if err != nil {
		t.Fatal(err)
	}
	if !cols["parent_chat_id"] {
		t.Fatal("chats has no `parent_chat_id` column after migrate")
	}
	if !cols["collected_at"] {
		t.Fatal("chats has no `collected_at` column after migrate")
	}

	var title string
	var parentChatID, collectedAt int64
	if err := db.QueryRow(
		`SELECT title, parent_chat_id, collected_at FROM chats WHERE workspace_id = 1`,
	).Scan(&title, &parentChatID, &collectedAt); err != nil {
		t.Fatalf("pre-existing chat row lost in migration: %v", err)
	}
	if title != "old chat" || parentChatID != 0 || collectedAt != 0 {
		t.Fatalf("migrated row wrong: title=%q parent_chat_id=%d collected_at=%d", title, parentChatID, collectedAt)
	}
}
