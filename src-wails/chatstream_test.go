package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

// captureTrimLog redirects the package logger for the duration of one trim
// call and returns what it printed — the smallest way to assert the hard cap
// is loud without threading a logger through trim's signature.
func captureTrimLog(t *testing.T, w *chatStreamWriter, chatID string, latestOrd int64) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	w.trim(chatID, latestOrd)
	return buf.String()
}

// drainChatStream blocks until the writer goroutine has flushed everything
// enqueued so far — the channel is FIFO and single-consumer, so a marker line
// that has landed proves the ones before it did too.
func drainChatStream(t *testing.T, a *App, chatID string) {
	t.Helper()
	w := a.chatStream()
	marker := "__drain__"
	ord := w.append(chatID, "claude-data", marker)
	for i := 0; i < 500; i++ {
		var n int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM chat_stream WHERE chat_id = ? AND ord = ?`, chatID, ord).Scan(&n); err != nil {
			t.Fatalf("drain query: %v", err)
		}
		if n == 1 {
			if _, err := a.db.Exec(`DELETE FROM chat_stream WHERE chat_id = ? AND ord = ?`, chatID, ord); err != nil {
				t.Fatalf("drain cleanup: %v", err)
			}
			return
		}
	}
	t.Fatal("chat stream writer did not flush")
}

func TestChatStreamReplayKeepsOrderAndKind(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	w.append("3", "claude-data", `{"i":0}`)
	w.append("3", "acp-req", `{"i":1}`)
	w.append("3", "claude-data", `{"i":2}`)
	w.append("9", "claude-data", `{"other":true}`)
	drainChatStream(t, a, "3")
	drainChatStream(t, a, "9")

	all, err := a.LoadChatStreamSince("3", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 lines for chat 3, got %d: %v", len(all), all)
	}
	for i, l := range all {
		if l.Ord != int64(i) {
			t.Fatalf("line %d has ord %d", i, l.Ord)
		}
		if l.Line != fmt.Sprintf(`{"i":%d}`, i) {
			t.Fatalf("line %d out of order: %s", i, l.Line)
		}
	}
	if all[1].Kind != "acp-req" {
		t.Fatalf("kind lost, want acp-req got %q", all[1].Kind)
	}

	// The point of the log: a client that stopped listening after ord 0 gets
	// exactly what it missed, nothing it already has.
	missed, err := a.LoadChatStreamSince("3", 1)
	if err != nil {
		t.Fatalf("load since: %v", err)
	}
	if len(missed) != 2 || missed[0].Ord != 1 {
		t.Fatalf("since=1 returned %v", missed)
	}
}

func TestChatStreamOrdSurvivesRestartAndDelete(t *testing.T) {
	a := newTestApp(t)
	a.chatStream().append("4", "claude-data", `{"i":0}`)
	drainChatStream(t, a, "4")

	// New writer over the same DB = app restart. Ord must continue, not reset,
	// or a replay would overwrite the crashed turn's lines.
	fresh := &App{db: a.db}
	fresh.chatStream().append("4", "claude-data", `{"i":1}`)
	drainChatStream(t, fresh, "4")
	lines, err := fresh.LoadChatStreamSince("4", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 || lines[1].Ord != 1 {
		t.Fatalf("ord did not resume across restart: %v", lines)
	}

	if err := fresh.DeleteChatMessages(4); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if lines, _ := fresh.LoadChatStreamSince("4", 0); len(lines) != 0 {
		t.Fatalf("delete left %d stream line(s)", len(lines))
	}
}

func TestTrimKeepsUnfoldedLines(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	// Two lines, both far older than any keep window would allow.
	w.append("5", "claude-data", `{"i":0}`)
	w.append("5", "claude-data", `{"i":1}`)
	drainChatStream(t, a, "5")

	// Nothing folded yet: age alone must not delete the only copy.
	w.trim("5", chatStreamKeep+10)
	if lines, _ := a.LoadChatStreamSince("5", 0); len(lines) != 2 {
		t.Fatalf("trim ate unfolded lines, %d left", len(lines))
	}

	// Frontend folds ord 0 (folded_ord = first UNfolded ord = 1) → only that
	// one becomes droppable.
	if err := a.SaveChatMessages(5, `[{"id":1}]`, 1); err != nil {
		t.Fatalf("save: %v", err)
	}
	w.trim("5", chatStreamKeep+10)
	lines, _ := a.LoadChatStreamSince("5", 0)
	if len(lines) != 1 || lines[0].Ord != 1 {
		t.Fatalf("want only the unfolded ord 1 left, got %v", lines)
	}

	got, err := a.ChatFoldedOrd("5")
	if err != nil || got != 1 {
		t.Fatalf("folded ord = %d, %v", got, err)
	}

	// A stale save must not walk the mark backwards — that would re-expose
	// lines the trim already dropped as if they were still pending.
	if err := a.SaveChatMessages(5, `[{"id":1}]`, 0); err != nil {
		t.Fatalf("save stale: %v", err)
	}
	if got, _ := a.ChatFoldedOrd("5"); got != 1 {
		t.Fatalf("folded ord regressed to %d", got)
	}
}

// TestHardCapTrimsPastFoldMarkerAndLogsLoudly drives trim with a latestOrd
// standing in for a chat that has produced far more than chatStreamHardKeep
// lines (inserting 200000 real rows would just be slow, not more honest),
// and a fold marker that never advanced — the noPersist-latch scenario the
// hard cap exists for. Two things must both be true: the growth bound
// actually holds (unfolded lines below the hard cutoff are deleted), and the
// deletion is not silent.
func TestHardCapTrimsPastFoldMarkerAndLogsLoudly(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	w.append("40", "claude-data", `{"i":0}`)
	w.append("40", "claude-data", `{"i":1}`)
	drainChatStream(t, a, "40")

	// No fold recorded at all: folded_ord is absent, so the ordinary
	// protection alone would keep both lines forever regardless of age.
	latestOrd := int64(chatStreamHardKeep + 10)
	got := captureTrimLog(t, w, "40", latestOrd)

	lines, err := a.LoadChatStreamSince("40", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Hard cutoff = latestOrd - chatStreamHardKeep = 10, so both ord 0 and 1
	// are below it and must be gone: the growth bound genuinely holds even
	// though nothing was folded.
	if len(lines) != 0 {
		t.Fatalf("hard cap did not bound growth, %d line(s) left: %v", len(lines), lines)
	}
	if !strings.Contains(got, "40") || !strings.Contains(got, "hard cap") {
		t.Fatalf("expected a loud hard-cap diagnostic naming the chat, got: %q", got)
	}
}

// TestUnderHardCapKeepsEveryUnfoldedLine is the same "fold marker far
// behind" shape as the hard-cap case above, but with too few lines to reach
// chatStreamHardKeep. The ordinary fold protection must still win, and the
// hard cap must stay silent — it only speaks when it is the one doing the
// deleting.
func TestUnderHardCapKeepsEveryUnfoldedLine(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	w.append("41", "claude-data", `{"i":0}`)
	w.append("41", "claude-data", `{"i":1}`)
	drainChatStream(t, a, "41")

	// Nothing folded, but latestOrd is only just past the routine keep
	// window, nowhere near the hard cap.
	got := captureTrimLog(t, w, "41", chatStreamKeep+10)

	lines, err := a.LoadChatStreamSince("41", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("trim ate unfolded lines under the hard cap, %d left", len(lines))
	}
	if got != "" {
		t.Fatalf("hard cap diagnostic fired when the hard cap did not act: %q", got)
	}
}

// TestRoutineTrimDoesNotLog is an ordinary trim of already-folded history —
// the hard cap never gets a chance to outrank anything, so it must not log.
// A diagnostic that fires on every trim is the same as no diagnostic.
func TestRoutineTrimDoesNotLog(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	w.append("42", "claude-data", `{"i":0}`)
	drainChatStream(t, a, "42")

	if err := a.SaveChatMessages(42, `[{"id":1}]`, 1); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := captureTrimLog(t, w, "42", chatStreamKeep+10)
	if got != "" {
		t.Fatalf("hard cap diagnostic fired on a routine trim: %q", got)
	}
}

func TestLoadChatEventsSinceReplaysAsDomainEvents(t *testing.T) {
	a := newTestApp(t)
	w := a.chatStream()
	w.append("6", "claude-data", `{"type":"assistant","message":{"id":"c1","content":[{"type":"text","text":"hi"}]}}`)
	w.append("6", "claude-data", `{"type":"system","subtype":"hook_started"}`)
	w.append("6", "acp-data", `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"hmm"}}}}`)
	drainChatStream(t, a, "6")

	got, err := a.LoadChatEventsSince("6", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The hook line carries no domain meaning, so it contributes no batch —
	// a client reading events sees a transcript, not the CLI's bookkeeping.
	if len(got) != 2 {
		t.Fatalf("want 2 batches, got %d: %+v", len(got), got)
	}
	if got[0].Ord != 0 || got[0].Events[0].Type != EvtTextDelta {
		t.Fatalf("first batch wrong: %+v", got[0])
	}
	if got[1].Ord != 2 || got[1].Events[0].Type != EvtThinkingDelta {
		t.Fatalf("second batch wrong: %+v", got[1])
	}
}
