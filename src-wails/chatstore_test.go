package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &App{db: db}
}

func TestSaveAndLoadChatMessagesRoundTrip(t *testing.T) {
	a := newTestApp(t)
	in := `[{"id":1,"role":"user","text":"ahoj"},{"id":2,"role":"assistant","text":"zdar"}]`
	if err := a.SaveChatMessages(7, in, -1); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := a.LoadChatMessages(7)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var got, want []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode out: %v (%s)", err, out)
	}
	_ = json.Unmarshal([]byte(in), &want)
	if len(got) != 2 || got[0]["text"] != "ahoj" || got[1]["text"] != "zdar" {
		t.Fatalf("round trip lost data: %s", out)
	}
}

// A save must replace the transcript, not append to the previous one.
func TestSaveChatMessagesReplacesPrevious(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveChatMessages(1, `[{"id":1},{"id":2},{"id":3}]`, -1); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.SaveChatMessages(1, `[{"id":9}]`, -1); err != nil {
		t.Fatalf("resave: %v", err)
	}
	out, _ := a.LoadChatMessages(1)
	var got []map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1: %s", len(got), out)
	}
}

// One chat's save must not touch another's — the whole point of leaving the
// single-blob config.json behind.
func TestSaveChatMessagesIsScopedToOneChat(t *testing.T) {
	a := newTestApp(t)
	_ = a.SaveChatMessages(1, `[{"id":1,"text":"keep"}]`, -1)
	_ = a.SaveChatMessages(2, `[{"id":2,"text":"other"}]`, -1)
	_ = a.SaveChatMessages(2, `[]`, -1)

	out, _ := a.LoadChatMessages(1)
	var got []map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if len(got) != 1 || got[0]["text"] != "keep" {
		t.Fatalf("chat 1 was clobbered by chat 2: %s", out)
	}
}

func TestLoadChatMessagesEmptyIsEmptyArray(t *testing.T) {
	a := newTestApp(t)
	out, err := a.LoadChatMessages(42)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out != "[]" {
		t.Fatalf("got %q, want []", out)
	}
}

func TestWriteFileAtomicLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := writeFileAtomic(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writeFileAtomic(path, []byte(`{"a":2}`)); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != `{"a":2}` {
		t.Fatalf("content = %s", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("temp files left behind: %v", names)
	}
}

func TestMigrateChatHistoryMovesTranscriptsAndShrinksConfig(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{"uiPrefs":{"scale":1.2},"chatMessageHistory":{"3":[{"id":1,"text":"prvni"}],"4":[{"id":1,"text":"a"},{"id":2,"text":"b"}]}}`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	a.migrateChatHistoryFrom(path)

	// Transcripts landed in SQLite.
	out, _ := a.LoadChatMessages(3)
	var got []map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if len(got) != 1 || got[0]["text"] != "prvni" {
		t.Fatalf("chat 3 not migrated: %s", out)
	}
	out4, _ := a.LoadChatMessages(4)
	_ = json.Unmarshal([]byte(out4), &got)
	if len(got) != 2 {
		t.Fatalf("chat 4 not migrated: %s", out4)
	}

	// The key is gone but every other preference survived.
	b, _ := os.ReadFile(path)
	var after map[string]any
	if err := json.Unmarshal(b, &after); err != nil {
		t.Fatalf("config.json is no longer valid JSON: %v", err)
	}
	if _, still := after["chatMessageHistory"]; still {
		t.Fatal("chatMessageHistory still in config.json")
	}
	if after["uiPrefs"] == nil {
		t.Fatal("migration dropped unrelated prefs")
	}

	// The original is recoverable.
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("no backup written: %v", err)
	}
}

// Re-running must not wipe transcripts (the key is gone, so it has to no-op).
func TestMigrateChatHistoryIsIdempotent(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte(`{"chatMessageHistory":{"1":[{"id":1,"text":"x"}]}}`), 0o644)

	a.migrateChatHistoryFrom(path)
	a.migrateChatHistoryFrom(path)

	out, _ := a.LoadChatMessages(1)
	var got []map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if len(got) != 1 {
		t.Fatalf("second run changed the transcript: %s", out)
	}
}

// A config.json with no chat history at all must be left completely alone.
func TestMigrateChatHistoryNoopWithoutKey(t *testing.T) {
	a := newTestApp(t)
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"uiPrefs":{"scale":1}}`
	_ = os.WriteFile(path, []byte(original), 0o644)

	a.migrateChatHistoryFrom(path)

	b, _ := os.ReadFile(path)
	if string(b) != original {
		t.Fatalf("config.json was rewritten: %s", b)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Fatal("wrote a backup for a no-op migration")
	}
}
