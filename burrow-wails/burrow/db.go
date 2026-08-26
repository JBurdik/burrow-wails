package main

import (
	"database/sql"
	"fmt"
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
	db, err := sql.Open("sqlite", dbPath)
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
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_opened TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS terminal_tabs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
			ord INTEGER NOT NULL DEFAULT 0,
			title TEXT,
			initial_cmd TEXT
		)`,
	}
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
		`ALTER TABLE terminal_tabs ADD COLUMN pty_id TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN cwd TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN default_title TEXT`,
		`ALTER TABLE terminal_tabs ADD COLUMN session_id TEXT`,
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
