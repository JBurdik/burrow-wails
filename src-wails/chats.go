package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// The chat list, owned by Go and stored in SQLite alongside workspaces and
// terminal_tabs.
//
// It used to live in config.json under `chatSessions`, which the desktop
// frontend cached whole at boot and rewrote whole on every setConfig, while
// Go's RemoteCreateChat did its own read-modify-write of the same file. Two
// writers on one blob with no merge meant last-writer-wins on EVERY key: a
// chat the phone had just created was reverted by the desktop saving a font
// preference, and the chatIdCounter went back with it, so the next desktop
// chat took the id the phone was already using — and inherited its running
// CLI process.
//
// Two properties here do the real work:
//
//   - AUTOINCREMENT: ids come from the database and are never reused. Plain
//     INTEGER PRIMARY KEY recycles the highest freed rowid, which would hand a
//     new chat the id of a deleted one whose chat_stream rows still exist.
//   - SaveChats upserts and NEVER deletes. A client whose list predates
//     another client's creation cannot remove what it has not heard of;
//     removal is an explicit DeleteChat.
//
// What is left, stated rather than hidden: two clients editing the same field
// of the same chat at the same moment still resolve last-writer-wins. That is
// per-chat-per-field instead of per-file-per-anything, and `chats-changed`
// makes both sides converge within one round trip.

// Chat is one chat session. Deliberately NOT here: `busy` and `status`.
// `busy` was always persisted as false anyway, and the status IS the phase
// (pty_phase, keyed `chat:<id>`) — a second copy of it in a second table is
// exactly the drift the phase work removed.
type Chat struct {
	ID              int64  `json:"id"`
	WorkspaceID     int64  `json:"workspace_id"`
	Title           string `json:"title"`
	PinnedTitle     bool   `json:"pinned_title"`
	ClaudeSessionID string `json:"claude_session_id"`
	MessageCount    int64  `json:"message_count"`
	Control         bool   `json:"control"`
	AgentKind       string `json:"agent_kind"`
	Transport       string `json:"transport"`
	Model           string `json:"model"`
	Branch          string `json:"branch"`
	// "" | "settled" | "active" — "" is the automatic bucket.
	SettledOverride string `json:"settled_override"`
	// 0 means not archived. A column, not a separate table: an archived chat
	// is still listed (the Sidebar has an Archived shelf) and unarchiving is
	// one UPDATE.
	ArchivedAt     int64 `json:"archived_at"`
	LastActivityAt int64 `json:"last_activity_at"`
}

func chatsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chats (
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
		)`,
		`CREATE INDEX IF NOT EXISTS chats_workspace ON chats(workspace_id)`,
	}
}

const chatColumns = `id, workspace_id, title, pinned_title, claude_session_id,
	message_count, control, agent_kind, transport, model, branch,
	settled_override, archived_at, last_activity_at`

func scanChat(rows interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Title, &c.PinnedTitle, &c.ClaudeSessionID,
		&c.MessageCount, &c.Control, &c.AgentKind, &c.Transport, &c.Model, &c.Branch,
		&c.SettledOverride, &c.ArchivedAt, &c.LastActivityAt)
	return c, err
}

// ListChats returns every chat, archived ones included — the client decides
// what to show, the same way it already does for terminal tabs.
func (a *App) ListChats() ([]Chat, error) {
	// Empty, never nil: a client maps over this without a guard, and JSON
	// null there is a broken first paint rather than an empty list.
	out := []Chat{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(`SELECT ` + chatColumns + ` FROM chats ORDER BY id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateChat inserts a chat and returns it with the id the DATABASE assigned.
// c.ID is ignored on purpose: a client inventing its own id is the bug this
// table exists to remove.
func (a *App) CreateChat(c Chat) (Chat, error) {
	if a.db == nil {
		return Chat{}, fmt.Errorf("no database")
	}
	res, err := a.db.Exec(
		`INSERT INTO chats (workspace_id, title, pinned_title, claude_session_id,
			message_count, control, agent_kind, transport, model, branch,
			settled_override, archived_at, last_activity_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.WorkspaceID, c.Title, c.PinnedTitle, c.ClaudeSessionID, c.MessageCount,
		c.Control, c.AgentKind, c.Transport, c.Model, c.Branch,
		c.SettledOverride, c.ArchivedAt, c.LastActivityAt,
	)
	if err != nil {
		return Chat{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Chat{}, err
	}
	c.ID = id
	busEmit("chats-changed", nil)
	return c, nil
}

// SaveChats upserts the rows a client holds. It never deletes: a client whose
// list predates another client's creation must not be able to remove a row it
// has never heard of, which is the whole failure this replaced.
//
// A row with a non-positive id is skipped rather than inserted — an id is the
// database's to hand out (CreateChat), and silently inserting here would give
// a client back the id-invention it just lost.
func (a *App) SaveChats(chats []Chat) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`UPDATE chats SET workspace_id=?, title=?, pinned_title=?, claude_session_id=?,
			message_count=?, control=?, agent_kind=?, transport=?, model=?, branch=?,
			settled_override=?, archived_at=?, last_activity_at=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range chats {
		if c.ID <= 0 {
			continue
		}
		if _, err := stmt.Exec(c.WorkspaceID, c.Title, c.PinnedTitle, c.ClaudeSessionID,
			c.MessageCount, c.Control, c.AgentKind, c.Transport, c.Model, c.Branch,
			c.SettledOverride, c.ArchivedAt, c.LastActivityAt, c.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	busEmit("chats-changed", nil)
	return nil
}

func (a *App) DeleteChat(id int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	res, err := a.db.Exec(`DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	busEmit("chats-changed", nil)
	return nil
}

// migrateChatsFromConfig moves the config.json chat list into SQLite, once.
//
// Ids are PRESERVED, not reassigned: chat_stream(chat_id), chat_messages and
// pty_phase's `chat:<id>` keys all reference them, so a renumbering migration
// would orphan every transcript in the app. Afterwards the AUTOINCREMENT
// sequence is advanced past the highest migrated id, or the first new chat
// takes an id a migrated chat already holds and inherits its transcript.
func (a *App) migrateChatsFromConfig() {
	if a.db == nil {
		return
	}
	var existing int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&existing); err != nil {
		log.Printf("chats migration: count: %v", err)
		return
	}
	if existing > 0 {
		return // already migrated
	}

	raw, err := a.ReadConfig()
	if err != nil {
		return
	}
	var cfg map[string]any
	if json.Unmarshal([]byte(raw), &cfg) != nil || cfg == nil {
		// config.json is hand-editable (Settings offers "edit this by hand").
		// A broken file must leave an empty chat list, not abort startup.
		return
	}
	sessions, _ := cfg["chatSessions"].([]any)
	if len(sessions) == 0 {
		// Nothing to move. Still drop the keys below so a later hand-edit of
		// config.json cannot look like it would be honoured.
		a.dropMigratedChatKeys(cfg)
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		log.Printf("chats migration: begin: %v", err)
		return
	}
	defer tx.Rollback()

	moved := 0
	for _, rawSession := range sessions {
		s, ok := rawSession.(map[string]any)
		if !ok {
			continue
		}
		c := chatFromConfigSession(s)
		if c.ID <= 0 {
			continue // no id to preserve; a row nothing can reference
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO chats (id, workspace_id, title, pinned_title,
				claude_session_id, message_count, control, agent_kind, transport,
				model, branch, settled_override, archived_at, last_activity_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.WorkspaceID, c.Title, c.PinnedTitle, c.ClaudeSessionID,
			c.MessageCount, c.Control, c.AgentKind, c.Transport, c.Model, c.Branch,
			c.SettledOverride, c.ArchivedAt, c.LastActivityAt,
		); err != nil {
			log.Printf("chats migration: insert %d: %v", c.ID, err)
			return
		}
		moved++
	}

	// No manual sqlite_sequence bump: SQLite advances the AUTOINCREMENT
	// sequence itself on any insert whose explicit rowid exceeds the stored
	// value, which every one of these does. TestMigrationAdvancesThe...
	// asserts it rather than trusting it, because getting this wrong hands
	// the first new chat a migrated chat's id — and its transcript.
	// (sqlite_sequence has no UNIQUE constraint, so an ON CONFLICT upsert
	// against it is not even legal.)
	if err := tx.Commit(); err != nil {
		log.Printf("chats migration: commit: %v", err)
		return
	}
	log.Printf("chats migration: moved %d chat(s) from config.json into SQLite", moved)
	a.dropMigratedChatKeys(cfg)
}

// dropMigratedChatKeys removes the now-authoritative-elsewhere keys, so
// config.json does not keep a stale copy that looks like the source of truth.
//
// A desktop frontend still holding a pre-migration in-memory cache can write
// them back once. Harmless: nothing reads them again.
func (a *App) dropMigratedChatKeys(cfg map[string]any) {
	_, hadSessions := cfg["chatSessions"]
	_, hadCounter := cfg["chatIdCounter"]
	if !hadSessions && !hadCounter {
		return
	}
	delete(cfg, "chatSessions")
	delete(cfg, "chatIdCounter")
	out, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	if err := a.WriteConfig(string(out)); err != nil {
		log.Printf("chats migration: prune config keys: %v", err)
	}
}

// chatFromConfigSession reads one config.json session object. Split out as a
// pure function so the field mapping is testable without a DB — it is the one
// place the old camelCase shape and the new column set meet.
func chatFromConfigSession(s map[string]any) Chat {
	num := func(key string) int64 {
		if v, ok := s[key].(float64); ok {
			return int64(v)
		}
		return 0
	}
	str := func(key string) string {
		if v, ok := s[key].(string); ok {
			return v
		}
		return ""
	}
	boolean := func(key string) bool {
		v, _ := s[key].(bool)
		return v
	}
	return Chat{
		ID:              num("id"),
		WorkspaceID:     num("workspaceId"),
		Title:           str("title"),
		PinnedTitle:     boolean("pinnedTitle"),
		ClaudeSessionID: str("claudeSessionId"),
		MessageCount:    num("messageCount"),
		Control:         boolean("control"),
		AgentKind:       str("agentKind"),
		Transport:       str("transport"),
		Model:           str("model"),
		Branch:          str("branch"),
		SettledOverride: str("settledOverride"),
		ArchivedAt:      num("archivedAt"),
		LastActivityAt:  num("lastActivityAt"),
	}
}
