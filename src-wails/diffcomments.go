package main

import (
	"fmt"
	"strings"
	"time"
)

// diff_comments backs the batch review flow (DiffTab.vue): a reviewer
// selects lines across several files, each selection becomes a row here
// with sent_at == 0, and "Send N notes" composes every pending row into one
// markdown message before marking them sent. SQLite rather than
// localStorage so the batch survives a restart and a paired phone can read
// it through the existing /v2/ws surface, same reasoning as chats.go.
//
// sent_at is a column, not a delete: after sending, the caller still wants
// to see what the agent was told (greyed out in the UI), so DeleteDiffComment
// deliberately does not exist yet — nothing in section 1 needs it.

// DiffComment is one reviewer note anchored to a diff line.
type DiffComment struct {
	ID        int64  `json:"id"`
	WsID      int64  `json:"ws_id"`
	File      string `json:"file"`
	Line      int64  `json:"line"`
	Side      string `json:"side"` // "additions" | "deletions", @pierre/diffs' AnnotationSide
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
	SentAt    int64  `json:"sent_at"` // 0 means unsent
}

func diffCommentsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS diff_comments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			ws_id      INTEGER NOT NULL,
			file       TEXT    NOT NULL,
			line       INTEGER NOT NULL,
			side       TEXT    NOT NULL DEFAULT '',
			body       TEXT    NOT NULL,
			created_at INTEGER NOT NULL,
			sent_at    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS diff_comments_ws ON diff_comments(ws_id)`,
	}
}

// ListDiffComments returns every note for a workspace, sent and unsent alike
// — the client decides what to grey out, the same pattern ListChats uses.
func (a *App) ListDiffComments(wsID int64) ([]DiffComment, error) {
	out := []DiffComment{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(
		`SELECT id, ws_id, file, line, side, body, created_at, sent_at
		 FROM diff_comments WHERE ws_id = ? ORDER BY created_at`, wsID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c DiffComment
		if err := rows.Scan(&c.ID, &c.WsID, &c.File, &c.Line, &c.Side, &c.Body, &c.CreatedAt, &c.SentAt); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddDiffComment records one reviewer note. The quoted diff excerpt is
// already folded into body by the caller (DiffTab.vue) — this table has no
// separate column for it, so composeDiffNotesMarkdown just prints body
// verbatim under the file:line header.
func (a *App) AddDiffComment(wsID int64, file string, line int64, side string, body string) (DiffComment, error) {
	if a.db == nil {
		return DiffComment{}, fmt.Errorf("no database")
	}
	if strings.TrimSpace(body) == "" {
		return DiffComment{}, fmt.Errorf("empty note")
	}
	now := time.Now().UnixMilli()
	res, err := a.db.Exec(
		`INSERT INTO diff_comments (ws_id, file, line, side, body, created_at, sent_at)
		 VALUES (?,?,?,?,?,?,0)`,
		wsID, file, line, side, body, now,
	)
	if err != nil {
		return DiffComment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return DiffComment{}, err
	}
	return DiffComment{ID: id, WsID: wsID, File: file, Line: line, Side: side, Body: body, CreatedAt: now}, nil
}

// diffCommentsByIDs fetches rows by id, in the order the ids were given —
// composeDiffNotesMarkdown's output order is the caller's (DiffTab.vue sends
// them oldest-first), not whatever SQLite happens to return.
func (a *App) diffCommentsByIDs(ids []int64) ([]DiffComment, error) {
	if a.db == nil {
		return nil, fmt.Errorf("no database")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := a.db.Query(
		`SELECT id, ws_id, file, line, side, body, created_at, sent_at
		 FROM diff_comments WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]DiffComment{}
	for rows.Next() {
		var c DiffComment
		if err := rows.Scan(&c.ID, &c.WsID, &c.File, &c.Line, &c.Side, &c.Body, &c.CreatedAt, &c.SentAt); err != nil {
			return nil, err
		}
		byID[c.ID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DiffComment, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// ComposeDiffNotes builds the single markdown message "Send N notes" posts.
func (a *App) ComposeDiffNotes(ids []int64) (string, error) {
	notes, err := a.diffCommentsByIDs(ids)
	if err != nil {
		return "", err
	}
	return composeDiffNotesMarkdown(notes), nil
}

// composeDiffNotesMarkdown is the pure formatting step, split out so it is
// testable with no database: file:line + side header, then the note body
// (which already carries the quoted hunk) verbatim.
func composeDiffNotesMarkdown(notes []DiffComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Review: %d note", len(notes))
	if len(notes) != 1 {
		b.WriteString("s")
	}
	b.WriteString("\n\n")
	for i, n := range notes {
		if i > 0 {
			b.WriteString("\n")
		}
		side := n.Side
		if side == "" {
			side = "additions"
		}
		fmt.Fprintf(&b, "### %s:%d (%s)\n\n%s\n", n.File, n.Line, side, strings.TrimSpace(n.Body))
	}
	return b.String()
}

// MarkDiffNotesSent greys the given notes out without deleting them — see
// the file-level comment for why sent notes stay.
func (a *App) MarkDiffNotesSent(ids []int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if len(ids) == 0 {
		return nil
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE diff_comments SET sent_at = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UnixMilli()
	for _, id := range ids {
		if _, err := stmt.Exec(now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
