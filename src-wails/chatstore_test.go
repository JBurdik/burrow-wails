package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	a := &App{db: db}
	t.Cleanup(func() {
		// The chat stream writer inserts on its own goroutine. Let its queue
		// land first, or a late INSERT hits a closed DB / recreates the WAL in
		// a TempDir that is being removed ("directory not empty").
		if a.streamW != nil {
			drainChatStream(t, a, "__cleanup__")
		}
		db.Close()
	})
	return a
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

func TestLoadChatMessagesPageWindowsFromTheTail(t *testing.T) {
	a := newTestApp(t)
	var in []string
	for i := 0; i < 10; i++ {
		in = append(in, fmt.Sprintf(`{"id":%d,"role":"user","text":"m%d"}`, i, i))
	}
	if err := a.SaveChatMessages(9, "["+strings.Join(in, ",")+"]", -1); err != nil {
		t.Fatal(err)
	}
	texts := func(p ChatMessagesPage) []string {
		var out []string
		for _, raw := range p.Messages {
			var m struct{ Text string }
			_ = json.Unmarshal(raw, &m)
			out = append(out, m.Text)
		}
		return out
	}
	newest, err := a.LoadChatMessagesPage(9, -1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if newest.Start != 6 || newest.Total != 10 || strings.Join(texts(newest), ",") != "m6,m7,m8,m9" {
		t.Fatalf("newest page: %+v %v", newest, texts(newest))
	}
	older, _ := a.LoadChatMessagesPage(9, newest.Start, 4)
	if older.Start != 2 || strings.Join(texts(older), ",") != "m2,m3,m4,m5" {
		t.Fatalf("older page: %+v %v", older, texts(older))
	}
	oldest, _ := a.LoadChatMessagesPage(9, older.Start, 4)
	if oldest.Start != 0 || strings.Join(texts(oldest), ",") != "m0,m1" {
		t.Fatalf("oldest page should be short and start at 0: %+v %v", oldest, texts(oldest))
	}
	empty, err := a.LoadChatMessagesPage(404, -1, 4)
	if err != nil || len(empty.Messages) != 0 || empty.Total != 0 {
		t.Fatalf("unknown chat: %+v %v", empty, err)
	}
}

// The in-memory fold is ahead of the table; a page must index into it the same way.
func TestLoadChatMessagesPageReadsTheLiveFold(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveChatMessages(9, `[{"id":0,"role":"user","text":"old0"},{"id":1,"role":"assistant","text":"old1"}]`, -1); err != nil {
		t.Fatal(err)
	}
	a.emitChatLine("9", chatUserKind, "live")
	p, err := a.LoadChatMessagesPage(9, -1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 3 || p.Start != 1 || len(p.Messages) != 2 || !strings.Contains(string(p.Messages[1]), "live") {
		t.Fatalf("page over the live fold: %+v", p)
	}
}

func TestPageStubsLargeImagesAndLoadChatImageReturnsThem(t *testing.T) {
	a := newTestApp(t)
	big := "data:image/png;base64," + strings.Repeat("A", chatImageStubMin)
	small := "data:image/png;base64,AAAA"
	in := fmt.Sprintf(`[{"id":0,"role":"assistant","text":"hi"},{"id":1,"role":"user","text":"look","images":[%q,%q]}]`, big, small)
	if err := a.SaveChatMessages(9, in, -1); err != nil {
		t.Fatal(err)
	}
	p, err := a.LoadChatMessagesPage(9, -1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Messages) != 2 || strings.Contains(string(p.Messages[1]), "AAAAAAAA") {
		t.Fatalf("large image must not travel in the page: %.200s", p.Messages[1])
	}
	var m struct {
		Text   string
		Images []string
	}
	_ = json.Unmarshal(p.Messages[1], &m)
	if m.Text != "look" || len(m.Images) != 2 || m.Images[0] != chatImageStubPrefix+"1/0" || m.Images[1] != small {
		t.Fatalf("want a stub for the big image and the small one inline: %+v", m)
	}
	got, err := a.LoadChatImage(9, 1, 0)
	if err != nil || got != big {
		t.Fatalf("LoadChatImage = %.40q, %v", got, err)
	}
	if _, err := a.LoadChatImage(9, 1, 5); err == nil {
		t.Fatal("an out-of-range image must be an error")
	}
	// The stored transcript is untouched: a full read still has the image.
	full, _ := a.LoadChatMessages(9)
	if !strings.Contains(full, big) {
		t.Fatal("stubbing must only affect the page, never the stored transcript")
	}
}
