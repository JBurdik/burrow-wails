package main

import (
	"database/sql"
	"strings"
)

// Full-text search over chat transcripts (Spotlight's chat section).
//
// chat_fts is an FTS5 index over the readable text of user and assistant
// messages in chat_messages — not chat_stream, which is raw protocol JSON and
// gets trimmed. It is kept in sync by TRIGGERS on chat_messages rather than by
// the Go writers, because there are several (the fold's upsert, SaveChatMessages'
// replace-all, the prune, DeleteChatMessages) and the next one would forget.
// The FTS rowid IS the chat_messages rowid: an upsert keeps it, so update and
// delete are a rowid lookup instead of a scan.
//
// modernc.org/sqlite ships FTS5 and JSON1 compiled in; no build tag needed.
func chatSearchSchema() []string {
	const indexed = `json_valid(new.payload_json) AND json_extract(new.payload_json, '$.role') IN ('user', 'assistant')`
	const text = `json_extract(new.payload_json, '$.text')`
	return []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS chat_fts USING fts5(text, tokenize = 'unicode61 remove_diacritics 2')`,
		`CREATE TRIGGER IF NOT EXISTS chat_fts_ai AFTER INSERT ON chat_messages WHEN ` + indexed + ` BEGIN
			INSERT INTO chat_fts(rowid, text) VALUES (new.rowid, ` + text + `);
		END`,
		`CREATE TRIGGER IF NOT EXISTS chat_fts_ad AFTER DELETE ON chat_messages BEGIN
			DELETE FROM chat_fts WHERE rowid = old.rowid;
		END`,
		`CREATE TRIGGER IF NOT EXISTS chat_fts_au AFTER UPDATE OF payload_json ON chat_messages BEGIN
			DELETE FROM chat_fts WHERE rowid = old.rowid;
			INSERT INTO chat_fts(rowid, text) SELECT new.rowid, ` + text + ` WHERE ` + indexed + `;
		END`,
	}
}

// migrateChatSearch creates the index and, only when it did not exist yet,
// backfills it from the transcripts already on disk. Idempotent: on every later
// start the table exists and the triggers have kept it current.
func migrateChatSearch(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'chat_fts'`).Scan(&n); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range chatSearchSchema() {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	if n == 0 {
		if _, err := tx.Exec(`INSERT INTO chat_fts(rowid, text)
			SELECT rowid, json_extract(payload_json, '$.text') FROM chat_messages
			WHERE json_valid(payload_json) AND json_extract(payload_json, '$.role') IN ('user', 'assistant')`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type ChatSearchHit struct {
	ChatID      int64  `json:"chat_id"`
	WorkspaceID int64  `json:"workspace_id"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	Ts          int64  `json:"ts"`
}

// ftsQuery turns what the user typed into an FTS5 query: every word must
// appear, the last one as a prefix (search-as-you-type). Words are quoted, so
// FTS syntax in the input (`AND`, `"`, `*`, `-`) is matched literally instead
// of erroring.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	if len(words) > 0 {
		words[len(words)-1] += "*"
	}
	return strings.Join(words, " ")
}

// SearchChats returns the best-matching message per chat, best chat first.
// Archived chats and orphaned transcripts (no chats row) are excluded.
// ponytail: plain-text snippet, no match markers — Spotlight shows it as one
// truncated line. Add markers to snippet() if a client ever highlights.
func (a *App) SearchChats(query string, limit int) ([]ChatSearchHit, error) {
	out := []ChatSearchHit{}
	q := ftsQuery(query)
	if a.db == nil || q == "" {
		return out, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	// ponytail: ranks every matching message, then keeps one per chat in Go.
	// Fine at personal-transcript scale; window functions if it ever shows up.
	rows, err := a.db.Query(`
		SELECT m.chat_id, c.workspace_id, c.title, c.last_activity_at,
		       snippet(chat_fts, 0, '', '', '…', 12)
		FROM chat_fts
		JOIN chat_messages m ON m.rowid = chat_fts.rowid
		JOIN chats c ON c.id = m.chat_id AND c.archived_at = 0
		WHERE chat_fts MATCH ?
		ORDER BY bm25(chat_fts)
		LIMIT 500`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() && len(out) < limit {
		var h ChatSearchHit
		if err := rows.Scan(&h.ChatID, &h.WorkspaceID, &h.Title, &h.Ts, &h.Snippet); err != nil {
			return nil, err
		}
		if seen[h.ChatID] {
			continue
		}
		seen[h.ChatID] = true
		out = append(out, h)
	}
	return out, rows.Err()
}
