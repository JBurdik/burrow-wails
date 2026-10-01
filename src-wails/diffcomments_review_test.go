package main

import (
	"database/sql"
	"strings"
	"testing"
)

func TestDiffCommentResolveReopenPreservesSendReceipt(t *testing.T) {
	a := &App{db: mustTestDB(t)}
	note, err := a.AddDiffComment(7, "old.go", 12, "additions", "Original quote and feedback")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.MarkDiffNotesSent([]int64{note.ID}); err != nil {
		t.Fatal(err)
	}
	if err = a.SetDiffCommentResolved(8, note.ID, true); err == nil {
		t.Fatal("another workspace could resolve the note")
	}
	if err = a.SetDiffCommentResolved(7, note.ID, true); err != nil {
		t.Fatal(err)
	}
	rows, err := a.ListDiffComments(7)
	if err != nil || len(rows) != 1 || rows[0].ResolvedAt == 0 || rows[0].SentAt == 0 || rows[0].Body != note.Body {
		t.Fatalf("resolved note: %+v, %v", rows, err)
	}
	sent := rows[0].SentAt
	composed, err := a.ComposeDiffNotes([]int64{note.ID})
	if err != nil || strings.Contains(composed, note.Body) {
		t.Fatalf("resolved note was composed: %q, %v", composed, err)
	}
	if err = a.SetDiffCommentResolved(7, note.ID, false); err != nil {
		t.Fatal(err)
	}
	rows, err = a.ListDiffComments(7)
	if err != nil || rows[0].ResolvedAt != 0 || rows[0].SentAt != sent {
		t.Fatalf("reopened note: %+v, %v", rows, err)
	}
}

func TestDiffCommentLegacyMigrationRetainsNotes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE diff_comments (id INTEGER PRIMARY KEY, ws_id INTEGER NOT NULL, file TEXT NOT NULL, line INTEGER NOT NULL, side TEXT NOT NULL DEFAULT '', body TEXT NOT NULL, created_at INTEGER NOT NULL, sent_at INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO diff_comments VALUES (1,7,'old.go',12,'additions','Existing review',1,2)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := (&App{db: db}).ListDiffComments(7)
	if err != nil || len(rows) != 1 || rows[0].Body != "Existing review" || rows[0].SentAt != 2 || rows[0].ResolvedAt != 0 {
		t.Fatalf("migration: %+v, %v", rows, err)
	}
}
