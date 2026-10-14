package main

import (
	"strings"
	"testing"
)

func searchIDs(t *testing.T, a *App, q string) []int64 {
	t.Helper()
	hits, err := a.SearchChats(q, 10)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	var ids []int64
	for _, h := range hits {
		ids = append(ids, h.ChatID)
	}
	return ids
}

func TestSearchChatsIndexesFollowsWritesAndDelete(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	c, _ := a.CreateChat(Chat{WorkspaceID: 3, Title: "refactor"})
	other, _ := a.CreateChat(Chat{WorkspaceID: 3, Title: "other"})
	if err := a.SaveChatMessages(int(c.ID), `[
		{"id":1,"role":"user","text":"Přepiš parser konfigurace"},
		{"id":2,"role":"tool","text":"Bash zebraword"},
		{"id":3,"role":"assistant","text":"Hotovo, parser je přepsaný"}]`, -1); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveChatMessages(int(other.ID), `[{"id":1,"role":"user","text":"unrelated"}]`, -1); err != nil {
		t.Fatal(err)
	}

	// One hit per chat, diacritics-insensitive, last word a prefix.
	hits, err := a.SearchChats("prepis kon", 10)
	if err != nil || len(hits) != 1 || hits[0].ChatID != c.ID || hits[0].WorkspaceID != 3 || hits[0].Title != "refactor" {
		t.Fatalf("hits = %+v, err = %v", hits, err)
	}
	if !strings.Contains(hits[0].Snippet, "parser") {
		t.Errorf("snippet misses the match: %q", hits[0].Snippet)
	}
	if ids := searchIDs(t, a, "parser"); len(ids) != 1 {
		t.Errorf("two matching messages must collapse into one chat: %v", ids)
	}
	if ids := searchIDs(t, a, "zebraword"); len(ids) != 0 {
		t.Errorf("tool rows must not be indexed: %v", ids)
	}
	// FTS syntax in the input is literal, not an error.
	searchIDs(t, a, `"AND * -(`)

	// Upsert path (the Go fold) replaces the indexed text.
	if _, err := a.db.Exec(`INSERT INTO chat_messages (chat_id, ord, payload_json) VALUES (?, 0, ?)
		ON CONFLICT(chat_id, ord) DO UPDATE SET payload_json = excluded.payload_json`,
		c.ID, `{"id":1,"role":"user","text":"kiwi"}`); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, a, "konfigurace"); len(ids) != 0 {
		t.Errorf("stale text still indexed after upsert: %v", ids)
	}
	if ids := searchIDs(t, a, "kiwi"); len(ids) != 1 {
		t.Errorf("upserted text not indexed: %v", ids)
	}

	if err := a.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, a, "kiwi"); len(ids) != 0 {
		t.Errorf("deleted chat still found: %v", ids)
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM chat_fts`).Scan(&n)
	if n != 1 {
		t.Errorf("chat_fts rows = %d, want 1 (only the other chat)", n)
	}
}

func TestMigrateChatSearchBackfillsOnce(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveChatMessages(5, `[{"id":1,"role":"assistant","text":"backfilled"}]`, -1); err != nil {
		t.Fatal(err)
	}
	// Simulate an install that predates the index.
	for _, s := range []string{`DROP TRIGGER chat_fts_ai`, `DROP TRIGGER chat_fts_ad`, `DROP TRIGGER chat_fts_au`, `DROP TABLE chat_fts`} {
		if _, err := a.db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateChatSearch(a.db); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM chat_fts WHERE chat_fts MATCH 'backfilled'`).Scan(&n)
	if n != 1 {
		t.Fatalf("backfill rows = %d, want exactly 1", n)
	}
}
