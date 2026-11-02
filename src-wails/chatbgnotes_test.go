package main

import (
	"encoding/json"
	"testing"
)

// Real Claude CLI 2.1.310 shapes (see chatbgtasks.go / backgroundTasks.ts).
const (
	bgLineToolUse     = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"pnpm dev --port 5173\nsecond line","run_in_background":true}}]}}`
	bgLineStartedBg   = `{"type":"system","subtype":"task_started","task_id":"b1","tool_use_id":"toolu_1","description":"Start dev server","is_backgrounded":true,"task_type":"local_bash"}`
	bgLineChanged     = `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1"}]}`
	bgLineUpdatedDone = `{"type":"system","subtype":"task_updated","task_id":"b1","patch":{"status":"completed","end_time":1761550000000}}`
	bgLineNotifyFail  = `{"type":"system","subtype":"task_notification","task_id":"b1","status":"completed","output_file":"/tmp/b1.output","summary":"Background command \"Start dev server\" completed (exit code 2)"}`

	fgLineToolUse   = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"go test ./..."}}]}}`
	fgLineStarted   = `{"type":"system","subtype":"task_started","task_id":"b2","tool_use_id":"toolu_2","description":"Run tests","is_backgrounded":false,"task_type":"local_bash"}`
	fgLineMoved     = `{"type":"system","subtype":"task_updated","task_id":"b2","patch":{"is_backgrounded":true}}`
	fgLineStopped   = `{"type":"system","subtype":"task_notification","task_id":"b2","status":"stopped","summary":"Background command \"Run tests\" was stopped"}`
	fgLineCompleted = `{"type":"system","subtype":"task_notification","task_id":"b2","status":"completed","summary":"Background command \"Run tests\" completed (exit code 0)"}`
)

// liveClaudeLine is what claudechat.go's reader does with each stdout line.
func liveClaudeLine(a *App, chat, line string) {
	a.emitChatLine(chat, "claude-data", line)
	a.noteBgTaskLine(chat, line)
}

func resetBgNotes() {
	bgNotes.Lock()
	bgNotes.chats = map[string]*bgNoteState{}
	bgNotes.Unlock()
}

func bgNoteTexts(t *testing.T, a *App, chat string) []chatNoteEnvelope {
	t.Helper()
	drainChatStream(t, a, chat)
	rows, err := a.db.Query(`SELECT line FROM chat_stream WHERE chat_id = ? AND kind = ? ORDER BY ord`, chat, chatNoteKind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []chatNoteEnvelope
	for rows.Next() {
		var line string
		var env chatNoteEnvelope
		if rows.Scan(&line) != nil || json.Unmarshal([]byte(line), &env) != nil {
			t.Fatalf("bad note row %q", line)
		}
		out = append(out, env)
	}
	return out
}

func TestBgNotesStartAndFailedFinishOnce(t *testing.T) {
	resetBgNotes()
	a := newTestApp(t)
	const chat = "11"
	for _, l := range []string{bgLineToolUse, bgLineStartedBg, bgLineChanged, bgLineStartedBg, bgLineUpdatedDone, bgLineNotifyFail, bgLineNotifyFail} {
		liveClaudeLine(a, chat, l)
	}
	got := bgNoteTexts(t, a, chat)
	want := []string{
		"Started in background: pnpm dev --port 5173",
		"Background task failed (exit 2): pnpm dev --port 5173",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d notes, got %+v", len(want), got)
	}
	for i, w := range want {
		if got[i].Text != w || got[i].Surface != "background" || got[i].Role != "system-info" {
			t.Fatalf("note %d = %+v, want %q", i, got[i], w)
		}
	}

	// A replay reads the log; it must not write notes again.
	if _, err := a.LoadChatEventsSince(chat, 0); err != nil {
		t.Fatal(err)
	}
	if n := len(bgNoteTexts(t, a, chat)); n != 2 {
		t.Fatalf("replay wrote notes: %d", n)
	}
}

func TestBgNotesForegroundMovedToBackground(t *testing.T) {
	resetBgNotes()
	a := newTestApp(t)
	const chat = "12"
	liveClaudeLine(a, chat, fgLineToolUse)
	liveClaudeLine(a, chat, fgLineStarted)
	if n := len(bgNoteTexts(t, a, chat)); n != 0 {
		t.Fatalf("a foreground task got a note: %d", n)
	}
	for _, l := range []string{fgLineMoved, fgLineMoved, fgLineStopped} {
		liveClaudeLine(a, chat, l)
	}
	got := bgNoteTexts(t, a, chat)
	if len(got) != 2 || got[0].Text != "Started in background: go test ./..." || got[1].Text != "Background task stopped: go test ./..." {
		t.Fatalf("notes = %+v", got)
	}
}

func TestBgNotesForegroundNeverBackgroundedStaysQuiet(t *testing.T) {
	resetBgNotes()
	a := newTestApp(t)
	const chat = "13"
	for _, l := range []string{fgLineToolUse, fgLineStarted, fgLineCompleted} {
		liveClaudeLine(a, chat, l)
	}
	if got := bgNoteTexts(t, a, chat); len(got) != 0 {
		t.Fatalf("notes = %+v", got)
	}
}

// A task backgrounded before an app restart: only the stream remembers it.
// Its finish is still noted; the start note (already in the log) is not repeated.
func TestBgNotesFinishAfterRestart(t *testing.T) {
	resetBgNotes()
	a := newTestApp(t)
	const chat = "14"
	for _, l := range []string{fgLineToolUse, fgLineStarted, fgLineMoved} {
		a.chatStream().append(chat, "claude-data", l) // the previous run's log, no notes
	}
	drainChatStream(t, a, chat)
	liveClaudeLine(a, chat, fgLineCompleted)
	liveClaudeLine(a, chat, fgLineCompleted)
	got := bgNoteTexts(t, a, chat)
	if len(got) != 1 || got[0].Text != "Background task finished: go test ./..." {
		t.Fatalf("notes = %+v", got)
	}
}

func TestBgNotesUnknownTaskIsIgnored(t *testing.T) {
	resetBgNotes()
	a := newTestApp(t)
	const chat = "15"
	liveClaudeLine(a, chat, bgLineNotifyFail)
	if got := bgNoteTexts(t, a, chat); len(got) != 0 {
		t.Fatalf("notes = %+v", got)
	}
}

func TestBgTaskLabelTruncates(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	st := &bgNoteState{commands: map[string]string{"u": long}}
	got := []rune(bgTaskLabel(st, &bgNoteTask{toolUseID: "u"}))
	if len(got) != 80 || got[79] != '…' {
		t.Fatalf("label = %q", string(got))
	}
	if l := bgTaskLabel(&bgNoteState{commands: map[string]string{}}, &bgNoteTask{description: "Run tests"}); l != "Run tests" {
		t.Fatalf("description fallback = %q", l)
	}
}
