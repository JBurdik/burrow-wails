package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// openDB opens (creating if needed) workspaces.db in the app data dir and
// applies the schema. Migrations are idempotent ALTER TABLE calls, same
// style as the Rust backend (src-tauri/src/lib.rs) — errors from an
// already-existing column are swallowed.
func openDB(appDataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir app data dir: %w", err)
	}
	dbPath := filepath.Join(appDataDir, "workspaces.db")
	// WAL + a busy timeout, because writers are no longer only the UI thread:
	// the chat stream log appends from its own goroutine while the frontend
	// reads. On the default rollback journal with no timeout that is an
	// immediate SQLITE_BUSY instead of a short wait. Both pragmas go in the
	// DSN so every pooled connection gets them; the path is URL-escaped
	// because the macOS app data dir contains a space.
	dsn := "file:" + (&url.URL{Path: dbPath}).String() +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS workspaces (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			last_opened INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS terminal_tabs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
			ord INTEGER NOT NULL DEFAULT 0,
			title TEXT,
			initial_cmd TEXT
		)`,
		// Dead schema: mission_tasks/agent_turns/task_attachments backed the removed
		// Kanban board + Mission Control. Kept so existing DBs still open (and their
		// rows survive) — nothing reads or writes them any more.
		`CREATE TABLE IF NOT EXISTS mission_tasks (
			id TEXT PRIMARY KEY,
			workspace_id INTEGER,
			pty_id INTEGER,
			title TEXT NOT NULL,
			cwd TEXT,
			model TEXT,
			status TEXT,
			turns INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS agent_turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL REFERENCES mission_tasks(id) ON DELETE CASCADE,
			pty_id INTEGER,
			worktree_path TEXT,
			started_at INTEGER NOT NULL,
			completed_at INTEGER,
			state TEXT NOT NULL DEFAULT 'running',
			start_tree TEXT,
			end_tree TEXT,
			changes_available INTEGER,
			change_error TEXT,
			files_json TEXT NOT NULL DEFAULT '[]',
			additions INTEGER,
			deletions INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_turns_task_id ON agent_turns(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_turns_pty_id ON agent_turns(pty_id)`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cwd TEXT NOT NULL,
			pty_id TEXT,
			label TEXT,
			commit_sha TEXT NOT NULL,
			tree_sha TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_checkpoints_cwd ON checkpoints(cwd, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS task_attachments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL REFERENCES mission_tasks(id) ON DELETE CASCADE,
			ord INTEGER NOT NULL DEFAULT 0,
			mime_type TEXT,
			file_path TEXT,
			created_at INTEGER NOT NULL
		)`,
		// One row per PTY or chat. The phase used to live in Terminal.vue, so
		// it existed only for a mounted workspace and an app restart threw it
		// away. Here it survives both.
		`CREATE TABLE IF NOT EXISTS pty_phase (
			id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			is_agent INTEGER NOT NULL DEFAULT 0,
			turn_ended_at INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0,
			-- Per-id monotonic counter so two concurrent Apply calls for the
			-- same id (hook server + foreground poll, different goroutines)
			-- can't have the older one win the DB row just by persisting last.
			seq INTEGER NOT NULL DEFAULT 0
		)`,
	}
	stmts = append(stmts, chatMessagesSchema()...)
	stmts = append(stmts, chatStreamSchema()...)
	stmts = append(stmts, remoteDevicesSchema()...)
	stmts = append(stmts, chatsSchema()...)
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}

	// Idempotent additive migrations, matching the Rust backend's columns.
	alters := []string{
		`ALTER TABLE workspaces ADD COLUMN parent_id INTEGER`,
		`ALTER TABLE workspaces ADD COLUMN worktree_branch TEXT`,
		`ALTER TABLE workspaces ADD COLUMN is_git INTEGER DEFAULT 0`,
		`ALTER TABLE workspaces ADD COLUMN icon TEXT`,
		`ALTER TABLE workspaces ADD COLUMN sort_order REAL DEFAULT 0`,
		`ALTER TABLE terminal_tabs ADD COLUMN pty_id INTEGER`,
		`ALTER TABLE terminal_tabs ADD COLUMN cwd TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN default_title TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN session_id TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN branch TEXT`,
		// Mirrored by setTabLiveStatus (phasestore.go) so `burrow list-tabs` /
		// MCP list_tabs can answer from SQLite alone — internal/control's
		// listTabs already selects this column.
		`ALTER TABLE terminal_tabs ADD COLUMN status TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN handed_off INTEGER DEFAULT 0`,
		`ALTER TABLE mission_tasks ADD COLUMN profile_id TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN repo_workspace_id INTEGER`,
		`ALTER TABLE mission_tasks ADD COLUMN board_column TEXT DEFAULT 'backlog'`,
		`ALTER TABLE mission_tasks ADD COLUMN description TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN agent_kind TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN transport TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN use_worktree INTEGER DEFAULT 1`,
		`ALTER TABLE mission_tasks ADD COLUMN worktree_branch TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN task_workspace_id INTEGER`,
		`ALTER TABLE mission_tasks ADD COLUMN chat_id INTEGER`,
		`ALTER TABLE mission_tasks ADD COLUMN session_id TEXT`,
		`ALTER TABLE mission_tasks ADD COLUMN board_order REAL DEFAULT 0`,
		`ALTER TABLE mission_tasks ADD COLUMN updated_at INTEGER`,
	}
	for _, s := range alters {
		if _, err := db.Exec(s); err != nil && !isDuplicateColumnErr(err) {
			return fmt.Errorf("%s: %w", s, err)
		}
	}

	return migratePtyPhaseColumns(db)
}

// migratePtyPhaseColumns repairs a `pty_phase` table created before its key
// column was renamed and `seq` was added.
//
// `CREATE TABLE IF NOT EXISTS` does not migrate an existing table, so every
// install that predates those changes kept `pty_id` and no `seq` while the code
// went on selecting `id` and `seq`. The failure was total and silent from the
// UI's side: NewPhaseStore returned an error, startup logged
// "phase store: SQL logic error: no such column: id" and carried on with
// a.phases == nil, so nothing persisted a phase, nothing emitted
// phase-pty:/phase-chat:, terminal_tabs.status was never written, and both
// clients' status dots — plus `burrow list-tabs` and MCP list_tabs — read
// empty forever.
//
// Done by column inspection rather than by running the ALTERs and ignoring the
// errors: a rename that is already applied fails with the same "no such column"
// shape as a genuinely broken table, so ignoring it would hide the very
// condition this exists to fix.
func migratePtyPhaseColumns(db *sql.DB) error {
	cols, err := tableColumns(db, "pty_phase")
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil // no table yet; the CREATE above already made the right one
	}
	if !cols["id"] && cols["pty_id"] {
		if _, err := db.Exec(`ALTER TABLE pty_phase RENAME COLUMN pty_id TO id`); err != nil {
			return fmt.Errorf("rename pty_phase.pty_id: %w", err)
		}
	}
	if !cols["seq"] {
		if _, err := db.Exec(`ALTER TABLE pty_phase ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`); err != nil && !isDuplicateColumnErr(err) {
			return fmt.Errorf("add pty_phase.seq: %w", err)
		}
	}
	return nil
}

// tableColumns returns the column names of a table, or an empty map when the
// table does not exist (PRAGMA table_info yields no rows rather than an error).
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("table_info %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func isDuplicateColumnErr(err error) bool {
	// modernc.org/sqlite surfaces SQLite's "duplicate column name" message
	// verbatim; ignore it the same way the Rust migrations swallow
	// "duplicate column name" from rusqlite.
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
