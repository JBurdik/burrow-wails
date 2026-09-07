package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func claudeTextLine(msgID, text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"content":[{"type":"text","text":%q}]}}`, msgID, text)
}

const claudeResultLine = `{"type":"result","subtype":"success"}`

func loadFolded(t *testing.T, a *App, chatID int) []ChatMessage {
	t.Helper()
	raw, err := a.LoadChatMessages(chatID)
	if err != nil {
		t.Fatalf("load chat messages: %v", err)
	}
	var msgs []ChatMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("decode transcript: %v (%s)", err, raw)
	}
	return msgs
}

func loadStored(t *testing.T, a *App, chatID int) []ChatMessage {
	t.Helper()
	raw, err := a.loadStoredChatMessages(chatID)
	if err != nil {
		t.Fatalf("load stored messages: %v", err)
	}
	var msgs []ChatMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("decode stored transcript: %v (%s)", err, raw)
	}
	return msgs
}

func storedFoldedOrd(t *testing.T, a *App, chatID string) (int64, bool) {
	t.Helper()
	var ord int64
	err := a.db.QueryRow(`SELECT folded_ord FROM chat_stream_state WHERE chat_id = ?`, chatID).Scan(&ord)
	if err != nil {
		return 0, false
	}
	return ord, true
}

// Task 2's acceptance: two lines through emitChatLine and the transcript is
// there, without any client having folded anything.
func TestGoFoldsTranscriptOnEmit(t *testing.T) {
	a := newTestApp(t)
	a.emitChatLine("5", chatUserKind, "ship it")
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "hotovo"))

	msgs := loadFolded(t, a, 5)
	if len(msgs) != 2 {
		t.Fatalf("want the folded pair, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Text != "ship it" {
		t.Fatalf("first message is not the prompt: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Text != "hotovo" {
		t.Fatalf("second message is not the reply: %+v", msgs[1])
	}
	if !msgs[1].Partial {
		t.Fatalf("a mid-turn reply should still be partial: %+v", msgs[1])
	}
	if msgs[0].ID != 0 || msgs[1].ID != 1 {
		t.Fatalf("ids are not the transcript positions: %d, %d", msgs[0].ID, msgs[1].ID)
	}

	// Coalescing: nothing has hit the table yet, and — the load-bearing half —
	// folded_ord has not moved either, so trim still protects the lines those
	// messages came from.
	if stored := loadStored(t, a, 5); len(stored) != 0 {
		t.Fatalf("fold wrote per line instead of coalescing: %+v", stored)
	}
	if _, ok := storedFoldedOrd(t, a, "5"); ok {
		t.Fatal("folded_ord advanced ahead of the stored transcript")
	}
}

// The marker and the messages move together: the turn boundary persists both,
// in one transaction, and settles every partial on the way.
func TestFoldPersistsMessagesAndFoldedOrdTogether(t *testing.T) {
	a := newTestApp(t)
	a.emitChatLine("5", chatUserKind, "ship it")
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "hotovo"))
	a.emitChatLine("5", "claude-data", claudeResultLine)

	stored := loadStored(t, a, 5)
	if len(stored) != 2 {
		t.Fatalf("turn boundary did not persist the transcript: %+v", stored)
	}
	for _, m := range stored {
		if m.Partial {
			t.Fatalf("a settled turn must not persist partial rows: %+v", m)
		}
	}
	ord, ok := storedFoldedOrd(t, a, "5")
	if !ok {
		t.Fatal("messages persisted without the folded_ord marker")
	}
	if ord != 3 {
		t.Fatalf("folded_ord = %d, want 3 (three lines folded)", ord)
	}
}

// A failed persist must leave NEITHER moved. Verified over the persist helper
// directly (dropping the table it writes) rather than through a
// fault-injecting SQLite wrapper.
func TestFoldPersistFailureMovesNeither(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	t2 := &chatTail{st: &foldState{messages: []ChatMessage{
		{ID: 0, Role: "user", Text: "ship it"},
		{ID: 1, Role: "assistant", Text: "hotovo"},
	}}, foldedOrd: 9, dirtyFrom: 0}
	w.mu.Lock()
	w.tails["5"] = t2
	w.mu.Unlock()

	if _, err := a.db.Exec(`DROP TABLE chat_messages`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	a.persistChatTail("5", t2, t2.combined())

	if _, ok := storedFoldedOrd(t, a, "5"); ok {
		t.Fatal("folded_ord committed although the messages could not be written")
	}
	if t2.dirtyFrom != 0 || t2.persistedOrd != 0 {
		t.Fatalf("a failed persist moved the tail's own bookkeeping: dirtyFrom=%d persistedOrd=%d", t2.dirtyFrom, t2.persistedOrd)
	}
}

// Task 5: the stored rows are the only surviving copy of a trimmed chat's
// history. They are adopted verbatim and the new fold is appended after them —
// nothing is recomputed from chat_stream below folded_ord.
func TestFoldAdoptsStoredTranscriptInsteadOfRecomputing(t *testing.T) {
	a := newTestApp(t)
	// A long chat as it exists on disk today: three rendered messages whose
	// stream lines (ords 0..99) the trim already deleted, and a marker saying
	// so.
	prior := `[{"id":1,"role":"user","text":"pradavna otazka"},` +
		`{"id":2,"role":"assistant","text":"pradavna odpoved"},` +
		`{"id":3,"role":"user","text":"a jeste jedna"}]`
	if err := a.SaveChatMessages(7, prior, 100); err != nil {
		t.Fatalf("seed stored transcript: %v", err)
	}
	// One surviving line, recorded but never folded — a turn in flight when
	// the app last exited.
	if _, err := a.db.Exec(
		`INSERT INTO chat_stream (chat_id, ord, kind, line) VALUES (?, ?, ?, ?)`,
		"7", 100, "claude-data", claudeTextLine("m9", "pokracuji"),
	); err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	a.emitChatLine("7", "claude-data", claudeTextLine("m9", " a hotovo"))

	msgs := loadFolded(t, a, 7)
	if len(msgs) != 4 {
		t.Fatalf("want 3 adopted + 1 folded, got %d: %+v", len(msgs), msgs)
	}
	// The adopted prefix is byte-for-byte what was stored, ids included:
	// nothing about it was re-derived.
	var want []ChatMessage
	if err := json.Unmarshal([]byte(prior), &want); err != nil {
		t.Fatal(err)
	}
	for i, m := range want {
		gotJSON, _ := json.Marshal(msgs[i])
		wantJSON, _ := json.Marshal(m)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("adopted message %d was recomputed: got %s want %s", i, gotJSON, wantJSON)
		}
	}
	// The catch-up from folded_ord and the live line are one bubble, appended
	// after the adopted rows.
	if msgs[3].Role != "assistant" || msgs[3].Text != "pokracuji a hotovo" {
		t.Fatalf("new fold is wrong: %+v", msgs[3])
	}
	// Ruling B: ids continue from max(stored id) + 1.
	if msgs[3].ID != 4 {
		t.Fatalf("id did not continue from the stored transcript: %d, want 4", msgs[3].ID)
	}
}

// A deleted chat must not leave its transcript behind in memory for a chat
// that reuses the id.
func TestDeleteChatMessagesForgetsTheFoldedTail(t *testing.T) {
	a := newTestApp(t)
	a.emitChatLine("5", chatUserKind, "ship it")
	if msgs := loadFolded(t, a, 5); len(msgs) != 1 {
		t.Fatalf("setup: %+v", msgs)
	}
	if err := a.DeleteChatMessages(5); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if msgs := loadFolded(t, a, 5); len(msgs) != 0 {
		t.Fatalf("recreated chat inherited a transcript: %+v", msgs)
	}
}

// The changed tail rides on the event, so a client splices instead of
// re-reading the whole transcript per token.
func TestFoldEmitsChangedTail(t *testing.T) {
	busReset()
	t.Cleanup(busReset)
	var mu sync.Mutex
	var got []ChatMessagesChanged
	unsub := busSubscribe(func(ev shellEvent) {
		if ev.Name != chatMessagesChangedPrefix+"5" {
			return
		}
		mu.Lock()
		got = append(got, ev.Payload.(ChatMessagesChanged))
		mu.Unlock()
	})
	t.Cleanup(unsub)

	a := newTestApp(t)
	a.emitChatLine("5", chatUserKind, "ship it")
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "ho"))
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "tovo"))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("want one event per changed line, got %d: %+v", len(got), got)
	}
	if got[0].FromIndex != 0 || len(got[0].Messages) != 1 {
		t.Fatalf("first event should carry just the new prompt: %+v", got[0])
	}
	if got[2].FromIndex != 1 || len(got[2].Messages) != 1 || got[2].Messages[0].Text != "hotovo" {
		t.Fatalf("a delta should carry only the message it grew: %+v", got[2])
	}
	if isRingable(chatMessagesChangedPrefix + "5") {
		t.Fatal("the transcript tail must stay out of the replay ring")
	}
}

// Two chats folding from their own goroutines share one chatStreamWriter and
// one tails map. Run under -race.
func TestFoldConcurrentChatsDoNotRace(t *testing.T) {
	a := newTestApp(t)
	var wg sync.WaitGroup
	for _, chatID := range []string{"11", "12"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			a.emitChatLine(id, chatUserKind, "start")
			for i := 0; i < 50; i++ {
				a.emitChatLine(id, "claude-data", claudeTextLine("m1", "x"))
			}
			a.emitChatLine(id, "claude-data", claudeResultLine)
		}(chatID)
	}
	wg.Wait()

	for _, chatID := range []string{"11", "12"} {
		id, _ := strconv.Atoi(chatID)
		msgs := loadFolded(t, a, id)
		if len(msgs) != 2 {
			t.Fatalf("chat %s: want prompt + one reply bubble, got %d", chatID, len(msgs))
		}
		if len(msgs[1].Text) != 50 {
			t.Fatalf("chat %s: lost deltas, text = %q", chatID, msgs[1].Text)
		}
		if ord, ok := storedFoldedOrd(t, a, chatID); !ok || ord != 52 {
			t.Fatalf("chat %s: folded_ord = %d (ok=%v), want 52", chatID, ord, ok)
		}
	}
}
