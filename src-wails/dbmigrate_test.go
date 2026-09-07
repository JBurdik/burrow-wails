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
