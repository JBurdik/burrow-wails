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
	}
	stmts = append(stmts, chatMessagesSchema()...)
	stmts = append(stmts, chatStreamSchema()...)
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
	return nil
}

func isDuplicateColumnErr(err error) bool {
	// modernc.org/sqlite surfaces SQLite's "duplicate column name" message
	// verbatim; ignore it the same way the Rust migrations swallow
	// "duplicate column name" from rusqlite.
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
