package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newChatApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := openDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Setenv("HOME", dir) // appDataDir() is HOME-derived; keep config.json in the temp dir
	return &App{db: db}, dir
}

func TestChatIdsAreNeverReused(t *testing.T) {
	// chat_stream(chat_id), chat_messages and pty_phase's `chat:<id>` rows can
	// outlive a deleted chat. A recycled id would hand a brand new chat
	// somebody else's transcript — which is why the table is AUTOINCREMENT
	// and not a plain INTEGER PRIMARY KEY.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	first, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteChat(first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID <= first.ID {
		t.Fatalf("id %d reused after deleting %d", second.ID, first.ID)
	}
}

func TestCreateChatIgnoresAClientSuppliedId(t *testing.T) {
	// A client inventing its own id is the bug this table removes; accepting
	// one here would hand that back.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	got, err := a.CreateChat(Chat{ID: 9999, WorkspaceID: 1, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == 9999 {
		t.Fatal("the client's id was honoured")
	}
}

func TestSaveChatsNeverDeletesARowItDoesNotKnowAbout(t *testing.T) {
	// THE bug. The desktop's list predates the phone's creation; it saves its
	// own rows; the phone's chat must survive. Under config.json this was a
	// whole-file overwrite and the phone's row was simply gone.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	desktop, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "desktop chat"})
	if err != nil {
		t.Fatal(err)
	}
	// The desktop reads its list HERE — before the phone creates anything.
	stale, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}

	phone, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "phone chat"})
	if err != nil {
		t.Fatal(err)
	}

	// ...and only then writes, with a list that has never heard of the phone.
	stale[0].Title = "desktop chat renamed"
	if err := a.SaveChats(stale); err != nil {
		t.Fatal(err)
	}

	after, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]Chat{}
	for _, c := range after {
		byID[c.ID] = c
	}
	if _, ok := byID[phone.ID]; !ok {
		t.Fatal("a stale save deleted the chat the other client created")
	}
	if byID[desktop.ID].Title != "desktop chat renamed" {
		t.Fatalf("the save did not take effect: %+v", byID[desktop.ID])
	}
}

func TestSaveChatsSkipsRowsWithNoId(t *testing.T) {
	// Inserting here would be id-invention by the back door.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	if err := a.SaveChats([]Chat{{ID: 0, WorkspaceID: 1, Title: "ghost"}}); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("save inserted a row with no id: %+v", list)
	}
}

func TestArchiveIsJustAColumnAndStillListed(t *testing.T) {
	// The Sidebar has an Archived shelf, so an archived chat has to come back
	// from ListChats — the client filters, the store does not hide.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	c, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "old"})
	if err != nil {
		t.Fatal(err)
	}
	c.ArchivedAt = 1234
	if err := a.SaveChats([]Chat{c}); err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListChats()
	if len(list) != 1 || list[0].ArchivedAt != 1234 {
		t.Fatalf("archived chat missing or not persisted: %+v", list)
	}
}

func TestChatListIsEmptyNotNull(t *testing.T) {
	a, _ := newChatApp(t)
	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(list)
	if string(blob) != "[]" {
		t.Fatalf("want [], got %s", blob)
	}
}

func TestMutationsEmitChatsChanged(t *testing.T) {
	// The event is what makes the other client converge without a reload; a
	// mutation that forgets it is the original bug wearing a new table.
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	var events []string
	busSubscribe(func(ev shellEvent) { events = append(events, ev.Name) })

	c, err := a.CreateChat(Chat{WorkspaceID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SaveChats([]Chat{c}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListChats(); err != nil { // a READ must not emit
		t.Fatal(err)
	}
	if err := a.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{"chats-changed", "chats-changed", "chats-changed"}
	if len(events) != len(want) {
		t.Fatalf("want %v, got %v", want, events)
	}
	for i, name := range events {
		if name != want[i] {
			t.Fatalf("event %d is %q, want %q (full: %v)", i, name, want[i], events)
		}
	}
}

func TestDeletingAnUnknownChatIsAnError(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	if err := a.DeleteChat(4242); err == nil {
		t.Fatal("deleting a chat that does not exist reported success")
	}
}

// --- migration ---

func writeTestConfig(t *testing.T, dir string, cfg map[string]any) {
	t.Helper()
	appDir := filepath.Join(dir, "Library", "Application Support", "burrow-wails")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestConfig(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "Library", "Application Support", "burrow-wails", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMigrationPreservesChatIds(t *testing.T) {
	// A renumbering migration orphans every chat_stream row in the app:
	// transcripts, folded_ord marks and phase keys all reference these ids.
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	writeTestConfig(t, dir, map[string]any{
		"chatIdCounter": float64(87),
		"chatSessions": []any{
			map[string]any{"id": float64(19), "workspaceId": float64(2), "title": "Chat 1", "transport": "claude-cli"},
			map[string]any{"id": float64(86), "workspaceId": float64(2), "title": "Chat 56", "agentKind": "claude", "control": true},
		},
	})

	a.migrateChatsFromConfig()

	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]Chat{}
	for _, c := range list {
		byID[c.ID] = c
	}
	if len(byID) != 2 {
		t.Fatalf("want 2 migrated chats, got %+v", list)
	}
	if byID[19].Title != "Chat 1" || byID[19].Transport != "claude-cli" {
		t.Fatalf("row 19 wrong: %+v", byID[19])
	}
	if byID[86].Title != "Chat 56" || byID[86].AgentKind != "claude" || !byID[86].Control {
		t.Fatalf("row 86 wrong: %+v", byID[86])
	}
}

func TestMigrationAdvancesTheAutoincrementPastMigratedIds(t *testing.T) {
	// Without this the first new chat takes an id a migrated chat already
	// holds — and inherits its transcript.
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	writeTestConfig(t, dir, map[string]any{
		"chatSessions": []any{
			map[string]any{"id": float64(86), "workspaceId": float64(2), "title": "Chat 56"},
		},
	})
	a.migrateChatsFromConfig()

	fresh, err := a.CreateChat(Chat{WorkspaceID: 2, Title: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID <= 86 {
		t.Fatalf("a new chat got id %d, colliding with migrated id 86", fresh.ID)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	writeTestConfig(t, dir, map[string]any{
		"chatSessions": []any{
			map[string]any{"id": float64(1), "workspaceId": float64(1), "title": "a"},
		},
	})
	a.migrateChatsFromConfig()
	// Re-writing the config and running again must not duplicate or revive:
	// a second run on a populated table is a no-op.
	writeTestConfig(t, dir, map[string]any{
		"chatSessions": []any{
			map[string]any{"id": float64(1), "workspaceId": float64(1), "title": "reverted"},
		},
	})
	a.migrateChatsFromConfig()

	list, _ := a.ListChats()
	if len(list) != 1 {
		t.Fatalf("want 1 chat, got %+v", list)
	}
	if list[0].Title != "a" {
		t.Fatalf("a second migration overwrote live data: %+v", list[0])
	}
}

func TestMigrationPrunesTheConfigKeys(t *testing.T) {
	// Leaving them behind leaves a stale copy that looks authoritative to the
	// next person reading config.json.
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	writeTestConfig(t, dir, map[string]any{
		"chatIdCounter": float64(2),
		"chatSessions": []any{
			map[string]any{"id": float64(1), "workspaceId": float64(1), "title": "a"},
		},
		"chatActiveByWs": map[string]any{"1": float64(1)},
	})
	a.migrateChatsFromConfig()

	cfg := readTestConfig(t, dir)
	if _, ok := cfg["chatSessions"]; ok {
		t.Fatal("chatSessions survived the migration")
	}
	if _, ok := cfg["chatIdCounter"]; ok {
		t.Fatal("chatIdCounter survived the migration")
	}
	// And it must not take the per-device keys with it: which chat is
	// selected is this device's business, not shared state.
	if _, ok := cfg["chatActiveByWs"]; !ok {
		t.Fatal("the migration deleted a per-device key it does not own")
	}
}

func TestMigrationToleratesAMissingOrGarbageKey(t *testing.T) {
	// config.json is hand-editable (Settings offers "edit this by hand"), so
	// a broken file has to leave an empty list rather than abort startup.
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	writeTestConfig(t, dir, map[string]any{"somethingElse": true})
	a.migrateChatsFromConfig()
	if list, err := a.ListChats(); err != nil || len(list) != 0 {
		t.Fatalf("missing key: %v %+v", err, list)
	}

	// A session that is not an object, and one with no id, are both skipped
	// rather than fatal — a row with no id is a row nothing can reference.
	writeTestConfig(t, dir, map[string]any{
		"chatSessions": []any{
			"not an object",
			map[string]any{"workspaceId": float64(1), "title": "no id"},
			map[string]any{"id": float64(5), "workspaceId": float64(1), "title": "fine"},
		},
	})
	a.migrateChatsFromConfig()
	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != 5 {
		t.Fatalf("want only the well-formed row, got %+v", list)
	}
}

// A child chat's parent must survive the round trip, or the Right Panel has no
// way to tell a sub-agent from a thread.
func TestParentChatIDRoundTrips(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.ParentChatID != 0 {
		t.Errorf("a top-level chat got parent %d, want 0", parent.ParentChatID)
	}
	child, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentChatID != parent.ID {
		t.Fatalf("CreateChat returned parent %d, want %d", child.ParentChatID, parent.ID)
	}

	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	var got int64 = -1
	for _, c := range list {
		if c.ID == child.ID {
			got = c.ParentChatID
		}
	}
	if got != parent.ID {
		t.Fatalf("ListChats reported parent %d, want %d", got, parent.ID)
	}
}

// SaveChats is the only path a client has for editing a row, so it has to carry
// the parent too — otherwise the first save after a spawn orphans the child.
func TestSaveChatsKeepsParent(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	child, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})

	child.Title = "renamed"
	if err := a.SaveChats([]Chat{child}); err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListChats()
	for _, c := range list {
		if c.ID == child.ID && c.ParentChatID != parent.ID {
			t.Fatalf("parent became %d after SaveChats, want %d", c.ParentChatID, parent.ID)
		}
	}
}

// Deleting a thread takes its sub-agents with it: they exist only under it, so
// leaving them behind leaves rows nothing in the UI can reach.
func TestDeleteChatCascadesToChildren(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	childA, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "a", ParentChatID: parent.ID})
	childB, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "b", ParentChatID: parent.ID})
	bystander, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "unrelated"})

	if err := a.DeleteChat(parent.ID); err != nil {
		t.Fatal(err)
	}

	list, _ := a.ListChats()
	alive := map[int64]bool{}
	for _, c := range list {
		alive[c.ID] = true
	}
	for _, gone := range []int64{parent.ID, childA.ID, childB.ID} {
		if alive[gone] {
			t.Errorf("chat %d survived the cascade", gone)
		}
	}
	if !alive[bystander.ID] {
		t.Error("the cascade took an unrelated chat")
	}
}
