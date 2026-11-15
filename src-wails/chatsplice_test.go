package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// spliceRecorder collects chat-messages-changed / chat-tail / chat-event for
// one chat, in emission order.
type spliceRecorder struct {
	mu     sync.Mutex
	order  []string // "splice" | "tail"
	splice []ChatSplice
	tails  []ChatTail
	events []ProviderRuntimeEvent
}

func recordSplices(t *testing.T, chatID string) *spliceRecorder {
	t.Helper()
	r := &spliceRecorder{}
	unsub := busSubscribe(func(ev shellEvent) {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch ev.Name {
		case chatMessagesChangedPrefix + chatID:
			r.order = append(r.order, "splice")
			r.splice = append(r.splice, ev.Payload.(ChatSplice))
		case chatTailPrefix + chatID:
			r.order = append(r.order, "tail")
			r.tails = append(r.tails, ev.Payload.(ChatTail))
		case "chat-event-" + chatID:
			r.events = append(r.events, ev.Payload.(ChatEventBatch).Events...)
		}
	})
	t.Cleanup(unsub)
	return r
}

func (r *spliceRecorder) snapshot() ([]string, []ChatSplice, []ChatTail) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...), append([]ChatSplice(nil), r.splice...), append([]ChatTail(nil), r.tails...)
}

func slowTails(t *testing.T, d time.Duration) {
	t.Helper()
	old := chatTailFlushAfter
	chatTailFlushAfter = d
	t.Cleanup(func() { chatTailFlushAfter = old })
}

func checkChain(t *testing.T, sp []ChatSplice) {
	t.Helper()
	for i := 1; i < len(sp); i++ {
		if sp[i].BaseRev != sp[i-1].Rev || sp[i].Rev <= sp[i-1].Rev {
			t.Fatalf("splice %d breaks the chain: prev rev %d, this %+v", i, sp[i-1].Rev, sp[i])
		}
	}
}

// Deltas into the streaming row are tails, not splices; a structural change
// flushes the pending tail first; revs chain; turnMs lands on the user row.
func TestSpliceRevSequenceAcrossDeltasAndTurnEnd(t *testing.T) {
	slowTails(t, time.Hour) // only a structural change may flush
	a := newTestApp(t)
	r := recordSplices(t, "5")

	a.emitChatLine("5", chatUserKind, "ship it")
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "ho"))
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "to"))
	a.emitChatLine("5", "claude-data", claudeTextLine("m1", "vo"))
	time.Sleep(5 * time.Millisecond)
	a.emitChatLine("5", "claude-data", claudeResultLine)

	order, sp, tails := r.snapshot()
	if got := strings.Join(order, ","); got != "splice,splice,tail,splice" {
		t.Fatalf("emission order = %s", got)
	}
	checkChain(t, sp)
	if tails[0].Index != 1 || tails[0].Append != "tovo" || tails[0].Offset != 2 || tails[0].Kind != "text" || tails[0].Rev != sp[1].Rev {
		t.Fatalf("tail = %+v (rev should be %d)", tails[0], sp[1].Rev)
	}
	last := sp[2]
	if last.FromIndex != 0 || len(last.Messages) != 2 || last.Messages[1].Text != "hotovo" || last.Messages[1].Partial {
		t.Fatalf("turn-end splice = %+v", last)
	}
	if last.Messages[0].TurnMs <= 0 {
		t.Fatalf("turnMs not stamped on the user row: %+v", last.Messages[0])
	}
	if ord, _ := storedFoldedOrd(t, a, "5"); last.Rev != ord {
		t.Fatalf("rev %d != folded_ord %d", last.Rev, ord)
	}
	page, _ := a.LoadChatMessagesPage(5, -1, 10)
	if page.Rev != last.Rev {
		t.Fatalf("page rev %d, last splice rev %d", page.Rev, last.Rev)
	}
	if isRingable(chatTailPrefix + "5") {
		t.Fatal("chat-tail must stay out of the replay ring")
	}
}

// N deltas inside one window → one tail event carrying the joined text.
func TestTailCoalescesWithinWindow(t *testing.T) {
	slowTails(t, 60*time.Millisecond)
	a := newTestApp(t)
	r := recordSplices(t, "6")
	a.emitChatLine("6", "claude-data", claudeTextLine("m1", "a"))
	for _, s := range []string{"b", "c", "d", "é"} {
		a.emitChatLine("6", "claude-data", claudeTextLine("m1", s))
	}
	time.Sleep(200 * time.Millisecond)
	_, sp, tails := r.snapshot()
	if len(sp) != 1 || len(tails) != 1 || tails[0].Append != "bcdé" || tails[0].Offset != 1 {
		t.Fatalf("splices %d, tails %+v", len(sp), tails)
	}
	// A tail never bumps rev: the next splice builds on the last splice's.
	a.emitChatLine("6", "claude-data", claudeResultLine)
	_, sp, _ = r.snapshot()
	checkChain(t, sp)
}

func TestChatClearKeepsOrdAndSendsAuthoritativeSplice(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	id := "1"
	r := recordSplices(t, id)
	a.emitChatLine(id, chatUserKind, "one")
	a.emitChatLine(id, "claude-data", claudeTextLine("m1", "two"))
	a.emitChatLine(id, "claude-data", claudeResultLine)
	before := a.chatStream().peekNextOrd(id)

	if err := a.ChatClear(c.ID); err != nil {
		t.Fatal(err)
	}
	_, sp, _ := r.snapshot()
	checkChain(t, sp)
	last := sp[len(sp)-1]
	if !last.Authoritative || last.FromIndex != 0 || len(last.Messages) != 0 {
		t.Fatalf("clear splice = %+v", last)
	}
	if ord := a.chatStream().append(id, "claude-data", `{"type":"system"}`); ord <= before {
		t.Fatalf("ord restarted after clear: %d <= %d", ord, before)
	}
	// And across a restart, with every stream line gone.
	drainChatStream(t, a, id)
	a.db.Exec(`DELETE FROM chat_stream WHERE chat_id = ?`, id)
	w := a.chatStream()
	w.mu.Lock()
	delete(w.nextOrd, id)
	w.mu.Unlock()
	if ord := w.append(id, "claude-data", `{"type":"system"}`); ord < last.Rev {
		t.Fatalf("ord after restart %d below rev %d", ord, last.Rev)
	}
}

func TestChatRewindSendsAuthoritativeSpliceFromCut(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", ClaudeSessionID: "sess-1"})
	saveFixture(t, a, c.ID, rewindFixture())
	a.emitChatLine("1", "claude-data", claudeResultLine) // something emitted before
	r := recordSplices(t, "1")
	revBefore := a.chatRev("1")
	if err := a.ChatRewind(c.ID, 3, "b"); err != nil {
		t.Fatal(err)
	}
	_, sp, _ := r.snapshot()
	last := sp[len(sp)-1]
	if !last.Authoritative || last.FromIndex != 3 || len(last.Messages) != 0 || last.Rev <= revBefore || last.BaseRev != revBefore {
		t.Fatalf("rewind splice = %+v (rev before %d)", last, revBefore)
	}
}

func TestChatSubscribeRingGapAndInProgress(t *testing.T) {
	slowTails(t, time.Hour)
	a := newTestApp(t)
	r := recordSplices(t, "7")
	a.emitChatLine("7", chatUserKind, "q")
	a.emitChatLine("7", "claude-data", claudeTextLine("m1", "par"))
	a.emitChatLine("7", "claude-data", claudeTextLine("m1", "tial"))
	_, sp, _ := r.snapshot()

	cur, err := a.ChatSubscribe(7, sp[len(sp)-1].Rev)
	if err != nil || len(cur.Splices) != 0 || cur.Rev != sp[len(sp)-1].Rev {
		t.Fatalf("up to date: %+v %v", cur, err)
	}
	if cur.InProgress == nil || cur.InProgress.Index != 1 || cur.InProgress.Text != "partial" {
		t.Fatalf("inProgress = %+v", cur.InProgress)
	}
	gap, _ := a.ChatSubscribe(7, sp[0].Rev)
	if len(gap.Splices) != 1 || gap.Splices[0].BaseRev != sp[0].Rev || gap.Splices[0].Authoritative {
		t.Fatalf("ring hit = %+v", gap.Splices)
	}
	miss, _ := a.ChatSubscribe(7, 12345)
	if len(miss.Splices) != 1 || !miss.Splices[0].Authoritative || miss.Splices[0].FromIndex != 0 || len(miss.Splices[0].Messages) != 2 {
		t.Fatalf("ring miss = %+v", miss.Splices)
	}
	// Ring of 64: the oldest falls out.
	for i := 0; i < chatSpliceRing+2; i++ {
		a.PublishChatNote("7", `{"form":"row","role":"system-info","text":"n"}`)
	}
	old, _ := a.ChatSubscribe(7, sp[0].Rev)
	if len(old.Splices) != 1 || !old.Splices[0].Authoritative {
		t.Fatalf("evicted rev should get an authoritative splice: %d splices", len(old.Splices))
	}
	// Not folded in this process: answers from the table.
	b := newTestApp(t)
	b.SaveChatMessages(9, `[{"id":0,"role":"user","text":"x"}]`, 4)
	cold, _ := b.ChatSubscribe(9, 0)
	if cold.Rev != 4 || len(cold.Splices) != 1 || !cold.Splices[0].Authoritative || len(cold.Splices[0].Messages) != 1 {
		t.Fatalf("cold subscribe = %+v", cold)
	}
}

func TestImagesTravelByReference(t *testing.T) {
	a := newTestApp(t)
	r := recordSplices(t, "8")
	img := "data:image/png;base64," + strings.Repeat("QUJD", 2000)
	small := "data:image/png;base64,QUJD"
	a.emitUserLine("8", "look", []string{img, small}, "c1")
	drainChatStream(t, a, "8")

	var line string
	a.db.QueryRow(`SELECT line FROM chat_stream WHERE chat_id = '8' AND kind = ?`, chatUserKind).Scan(&line)
	if strings.Contains(line, "base64") || !strings.Contains(line, chatImageRefPrefix) {
		t.Fatalf("chat-user line carries data: %.200s", line)
	}
	_, sp, _ := r.snapshot()
	want := []string{"burrow-image://0/0", "burrow-image://0/1"}
	if got := sp[0].Messages[0].Images; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("splice images = %v", got)
	}
	page, _ := a.LoadChatMessagesPage(8, -1, 10)
	full, _ := a.LoadChatMessages(8)
	for name, s := range map[string]string{"page": string(page.Messages[0]), "load": full} {
		if strings.Contains(s, "base64") || strings.Contains(s, chatImageRefPrefix) {
			t.Fatalf("%s leaks data/refs: %.200s", name, s)
		}
	}
	for n, wantData := range []string{img, small} {
		got, err := a.LoadChatImage(8, 0, n)
		if err != nil || got != wantData {
			t.Fatalf("load_chat_image %d: %v (%d bytes)", n, err, len(got))
		}
	}
	// No client folds chat-event any more: its user.delta carries refs, not data.
	r.mu.Lock()
	ev := r.events[0]
	r.mu.Unlock()
	if len(ev.Images) != 2 || !strings.HasPrefix(ev.Images[0], chatImageRefPrefix) {
		t.Fatalf("chat-event user.delta images = %.80v", ev.Images)
	}
	// An old row with an inline data URL is stubbed in splices too.
	a.PublishChatNote("8", `{"form":"row","role":"system-info","text":"x","images":["`+img+`"]}`)
	_, sp, _ = r.snapshot()
	if b, _ := json.Marshal(sp[len(sp)-1]); strings.Contains(string(b), "base64") {
		t.Fatalf("note splice carries data: %.200s", b)
	}
}

func TestChatSendImagesReachCLIInFull(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	img := "data:image/png;base64," + strings.Repeat("QUJD", 2000)
	if err := a.ChatSend(c.ID, "see", []string{img}, ""); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	sent := strings.Join(fake.sent, "\n")
	fake.mu.Unlock()
	if !strings.Contains(sent, strings.Repeat("QUJD", 2000)) {
		t.Fatal("the CLI did not get the image data")
	}
}

func TestPermissionReceiptWrittenByGo(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if err := a.ChatSend(c.ID, "go", nil, ""); err != nil {
		t.Fatal(err)
	}
	a.emitChatLine("1", "claude-data", `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls -la"}}}`)
	if err := a.ClaudeRespondControl("1", "r1", map[string]any{"behavior": "allow", "updatedInput": map[string]any{"command": "ls -la"}}); err != nil {
		t.Fatal(err)
	}
	a.emitChatLine("1", "claude-data", `{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{}}}`)
	a.ClaudeRespondControl("1", "r2", map[string]any{"behavior": "allow", "updatedInput": map[string]any{}})
	msgs := loadFolded(t, a, 1)
	var receipts []string
	for _, m := range msgs {
		if m.Role == "permission" {
			receipts = append(receipts, m.Text)
		}
	}
	if len(receipts) != 1 || receipts[0] != "✓ Allowed: Bash — ls -la" {
		t.Fatalf("receipts = %q", receipts)
	}
}

// A line recorded before images went by reference replays with refs: the
// raw replay and the event replay both.
func TestStreamReplayRefsOldInlineImages(t *testing.T) {
	a := newTestApp(t)
	img := "data:image/png;base64," + strings.Repeat("QUJD", 2000)
	a.db.Exec(`INSERT INTO chat_stream (chat_id, ord, kind, line) VALUES ('5', 0, ?, ?), ('5', 1, ?, ?)`,
		chatUserKind, encodeUserLine("old", []string{img}, "c9"),
		chatNoteKind, `{"form":"row","role":"system-info","text":"n","images":["`+img+`"]}`)
	lines, err := a.LoadChatStreamSince("5", 0)
	if err != nil || len(lines) != 2 {
		t.Fatalf("replay: %v %d", err, len(lines))
	}
	for _, l := range lines {
		if strings.Contains(l.Line, "base64") || !strings.Contains(l.Line, chatImageRefPrefix) {
			t.Fatalf("%s replays data: %.120s", l.Kind, l.Line)
		}
	}
	if u := decodeUserLine(lines[0].Line); u.Text != "old" || u.ClientID != "c9" {
		t.Fatalf("user line mangled: %+v", u)
	}
	evs, _ := a.LoadChatEventsSince("5", 0)
	if b, _ := json.Marshal(evs); strings.Contains(string(b), "base64") {
		t.Fatalf("event replay carries data: %.200s", b)
	}
	if got, err := a.resolveChatImage(5, decodeUserLine(lines[0].Line).Images[0]); err != nil || got != img {
		t.Fatalf("stored ref does not resolve: %v", err)
	}
}

// The ring is bounded by bytes too: big rows evict the oldest splices, and
// the newest one always stays.
func TestSpliceRingBoundedByBytes(t *testing.T) {
	lg := &chatSpliceLog{tailFrom: -1}
	big := strings.Repeat("x", chatSpliceRingBytes/3)
	for i := range 5 {
		lg.push(ChatSplice{BaseRev: int64(i), Rev: int64(i + 1), Messages: []ChatMessage{{Role: "tool", ToolOutput: big}}})
	}
	if len(lg.ring) != 2 || lg.ring[0].Rev != 4 || lg.bytes > chatSpliceRingBytes {
		t.Fatalf("ring = %d splices, %d bytes, oldest rev %d", len(lg.ring), lg.bytes, lg.ring[0].Rev)
	}
	lg.push(ChatSplice{BaseRev: 5, Rev: 6, Messages: []ChatMessage{{Role: "tool", ToolOutput: strings.Repeat("y", 2*chatSpliceRingBytes)}}})
	if len(lg.ring) != 1 || lg.ring[0].Rev != 6 || len(lg.sizes) != 1 {
		t.Fatalf("an oversized splice should be kept alone: %d", len(lg.ring))
	}
}
