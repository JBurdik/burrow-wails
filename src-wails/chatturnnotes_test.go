package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Real Claude CLI stream-json shapes.
const (
	tnClaudeTodo1  = `{"type":"assistant","uuid":"u1","message":{"id":"m1","content":[{"type":"tool_use","id":"toolu_t1","name":"TodoWrite","input":{"todos":[{"content":"Read code","status":"completed","activeForm":"Reading code"},{"content":"Write fix","status":"in_progress","activeForm":"Writing fix"},{"content":"Test","status":"pending","activeForm":"Testing"}]}}]}}`
	tnClaudeTodo1R = `{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_t1","content":"Todos have been modified successfully."}]}}`
	tnClaudeTodo2  = `{"type":"assistant","uuid":"u3","message":{"id":"m2","content":[{"type":"tool_use","id":"toolu_t2","name":"TodoWrite","input":{"todos":[{"content":"Read code","status":"completed"},{"content":"Write fix","status":"completed"},{"content":"Test","status":"in_progress"}]}}]}}`
	tnClaudeWrite  = `{"type":"assistant","uuid":"u4","message":{"id":"m3","content":[{"type":"tool_use","id":"toolu_w","name":"Write","input":{"file_path":"/repo/src/new.ts","content":"x"}}]}}`
	tnClaudeWriteR = `{"type":"user","uuid":"u5","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_w","content":"File created successfully at: /repo/src/new.ts"}]}}`
	tnClaudeEdit   = `{"type":"assistant","uuid":"u6","message":{"id":"m4","content":[{"type":"tool_use","id":"toolu_e1","name":"Edit","input":{"file_path":"/repo/src/a.ts","old_string":"a","new_string":"b"}}]}}`
	tnClaudeEditR  = `{"type":"user","uuid":"u7","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_e1","content":"The file /repo/src/a.ts has been updated."}]}}`
	// Same file again: distinct paths, not calls.
	tnClaudeEdit2  = `{"type":"assistant","uuid":"u8","message":{"id":"m5","content":[{"type":"tool_use","id":"toolu_e2","name":"MultiEdit","input":{"file_path":"/repo/src/a.ts","edits":[]}}]}}`
	tnClaudeEdit2R = `{"type":"user","uuid":"u9","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_e2","content":"ok"}]}}`
	// A failed edit changed nothing.
	tnClaudeEditBad   = `{"type":"assistant","uuid":"u10","message":{"id":"m6","content":[{"type":"tool_use","id":"toolu_e3","name":"Edit","input":{"file_path":"/repo/src/b.ts","old_string":"zz","new_string":"y"}}]}}`
	tnClaudeEditBadR  = `{"type":"user","uuid":"u11","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_e3","is_error":true,"content":"<tool_use_error>String to replace not found in file.</tool_use_error>"}]}}`
	tnClaudeText      = `{"type":"assistant","uuid":"u12","message":{"id":"m7","content":[{"type":"text","text":"Done."}]}}`
	tnClaudeResult    = `{"type":"result","subtype":"success","result":"Done.","usage":{"input_tokens":10,"output_tokens":5}}`
	tnClaudeResultBad = `{"type":"result","subtype":"error_during_execution","result":"boom"}`

	// Codex lines as pumpCodexLine stores them on acp-data.
	tnCodexPlan1   = `{"method":"session/update","params":{"update":{"entries":[{"content":"Inspect","priority":"medium","status":"in_progress"},{"content":"Patch","priority":"medium","status":"pending"}],"sessionUpdate":"plan"}}}`
	tnCodexPlan2   = `{"method":"session/update","params":{"update":{"entries":[{"content":"Inspect","priority":"medium","status":"completed"},{"content":"Patch","priority":"medium","status":"in_progress"}],"sessionUpdate":"plan"}}}`
	tnCodexPatch   = `{"method":"session/update","params":{"update":{"input":{"changes":[{"diff":"@@ -18,3 +18,3 @@\n","kind":{"move_path":null,"type":"update"},"path":"/repo/compose.yml"},{"diff":"","kind":{"type":"add"},"path":"/repo/new.go"}]},"sessionUpdate":"tool_call","title":"Apply file changes","toolCallId":"exec-1"}}}`
	tnCodexPatchOK = `{"method":"session/update","params":{"update":{"content":[{"content":{"text":"","type":"text"}}],"sessionUpdate":"tool_call_update","status":"completed","toolCallId":"exec-1"}}}`
	tnCodexDecl    = `{"method":"session/update","params":{"update":{"input":{"changes":[{"diff":"","kind":{"type":"delete"},"path":"/repo/gone.go"}]},"sessionUpdate":"tool_call","title":"Apply file changes","toolCallId":"exec-2"}}}`
	tnCodexDeclR   = `{"method":"session/update","params":{"update":{"content":[{"content":{"text":"","type":"text"}}],"sessionUpdate":"tool_call_update","status":"failed","toolCallId":"exec-2"}}}`
	tnCodexText    = `{"method":"session/update","params":{"update":{"content":{"text":"Patched."},"messageId":"msg_1","sessionUpdate":"agent_message_chunk"}}}`
	tnCodexTurn    = `{"_burrow":"turn"}`
)

type tnRow struct {
	kind string
	env  chatNoteEnvelope
	text string
}

// tnTranscript reads back every row in the chat's stream, in order.
func tnTranscript(t *testing.T, a *App, chat string) []tnRow {
	t.Helper()
	drainChatStream(t, a, chat)
	rows, err := a.db.Query(`SELECT kind, line FROM chat_stream WHERE chat_id = ? ORDER BY ord`, chat)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []tnRow
	for rows.Next() {
		var r tnRow
		if err := rows.Scan(&r.kind, &r.text); err != nil {
			t.Fatal(err)
		}
		if r.kind == chatNoteKind {
			if err := json.Unmarshal([]byte(r.text), &r.env); err != nil {
				t.Fatalf("bad note %q", r.text)
			}
		}
		out = append(out, r)
	}
	return out
}

// tnNotes: "surface: text" of every surface note, in transcript order.
func tnNotes(rows []tnRow) []string {
	var out []string
	for _, r := range rows {
		if r.kind == chatNoteKind && r.env.Surface != "" {
			out = append(out, r.env.Surface+": "+r.env.Text)
		}
	}
	return out
}

func tnEmit(a *App, chat, kind string, lines ...string) {
	for _, l := range lines {
		a.emitChatLine(chat, kind, l)
	}
}

func TestTurnNotesClaudeFirstPlanAndChangedFiles(t *testing.T) {
	a := newTestApp(t)
	tnEmit(a, "11", chatUserKind, "fix it")
	tnEmit(a, "11", "claude-data",
		tnClaudeTodo1, tnClaudeTodo1R,
		tnClaudeWrite, tnClaudeWriteR,
		tnClaudeEdit, tnClaudeEditR,
		tnClaudeEdit2, tnClaudeEdit2R,
		tnClaudeEditBad, tnClaudeEditBadR,
		tnClaudeTodo2, // a later plan in the same turn: no second note
		tnClaudeText, tnClaudeResult,
		`{"type":"exit"}`, // a second turn end must not repeat the note
	)
	rows := tnTranscript(t, a, "11")
	want := []string{
		"progress: Plan: 1/3 — Write fix",
		"outputs: Changed 2 files", // new.ts + a.ts; b.ts failed
	}
	if got := tnNotes(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("notes = %q, want %q", got, want)
	}
	// The plan note follows the TodoWrite line; the outputs note follows the
	// turn's last assistant text and its result.
	idx := func(text string) int {
		for i, r := range rows {
			if r.text == text || (r.kind == chatNoteKind && r.env.Surface+": "+r.env.Text == text) {
				return i
			}
		}
		return -1
	}
	if !(idx(tnClaudeTodo1) < idx(want[0]) && idx(want[0]) < idx(tnClaudeTodo1R)) {
		t.Fatalf("plan note out of place: %d %d %d", idx(tnClaudeTodo1), idx(want[0]), idx(tnClaudeTodo1R))
	}
	if !(idx(tnClaudeText) < idx(tnClaudeResult) && idx(tnClaudeResult) < idx(want[1])) {
		t.Fatalf("outputs note out of place")
	}

	// The next turn starts fresh: its first plan gets a note again.
	tnEmit(a, "11", chatUserKind, "again")
	tnEmit(a, "11", "claude-data", tnClaudeTodo2, tnClaudeEdit, tnClaudeEditR, tnClaudeResultBad)
	got := tnNotes(tnTranscript(t, a, "11"))
	want = append(want, "progress: Plan: 2/3 — Test", "outputs: Changed a.ts")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second turn notes = %q, want %q", got, want)
	}
}

func TestTurnNotesNothingChangedNoOutputsNote(t *testing.T) {
	a := newTestApp(t)
	tnEmit(a, "12", chatUserKind, "look")
	tnEmit(a, "12", "claude-data", tnClaudeEditBad, tnClaudeEditBadR, tnClaudeText, tnClaudeResult)
	if got := tnNotes(tnTranscript(t, a, "12")); len(got) != 0 {
		t.Fatalf("notes = %q", got)
	}
}

func TestTurnNotesCodex(t *testing.T) {
	a := newTestApp(t)
	tnEmit(a, "13", chatUserKind, "patch it")
	tnEmit(a, "13", "acp-data",
		tnCodexPlan1, tnCodexPatch, tnCodexPatchOK,
		tnCodexDecl, tnCodexDeclR, // declined: gone.go not counted
		tnCodexPlan2, tnCodexText, tnCodexTurn,
	)
	want := []string{"progress: Plan: 0/2 — Inspect", "outputs: Changed 2 files"}
	if got := tnNotes(tnTranscript(t, a, "13")); !reflect.DeepEqual(got, want) {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

// Lines outside a turn (an ACP session/load history replay) and a turn cut by
// session.reset produce nothing; replaying the stored stream adds nothing.
func TestTurnNotesOnlyForLiveOpenTurns(t *testing.T) {
	a := newTestApp(t)
	// History replay: no prompt opened a turn.
	tnEmit(a, "14", "acp-data", tnCodexPlan1, tnCodexPatch, tnCodexPatchOK, tnCodexTurn)
	// A turn cut by a restart: no outputs note for it.
	tnEmit(a, "14", chatUserKind, "go")
	tnEmit(a, "14", "acp-data", tnCodexPatch, tnCodexPatchOK)
	tnEmit(a, "14", chatRuntimeKind, "reset")
	tnEmit(a, "14", "acp-data", tnCodexTurn)
	if got := tnNotes(tnTranscript(t, a, "14")); len(got) != 0 {
		t.Fatalf("notes = %q", got)
	}

	// One real turn, then a replay of the whole stream.
	tnEmit(a, "14", chatUserKind, "go")
	tnEmit(a, "14", "acp-data", tnCodexPlan1, tnCodexPatch, tnCodexPatchOK, tnCodexTurn)
	before := tnNotes(tnTranscript(t, a, "14"))
	if len(before) != 2 {
		t.Fatalf("notes = %q", before)
	}
	batches, err := a.LoadChatEventsSince("14", 0)
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	for _, b := range batches {
		for _, e := range b.Events {
			if e.Type == EvtMessageNote && e.Surface != "" {
				notes++
			}
		}
	}
	if notes != 2 {
		t.Fatalf("replay saw %d surface notes, want the 2 recorded ones", notes)
	}
	if after := tnNotes(tnTranscript(t, a, "14")); !reflect.DeepEqual(after, before) {
		t.Fatalf("replay wrote notes: %q", after)
	}
}

func TestPlanNoteText(t *testing.T) {
	for _, c := range []struct {
		todos []any
		want  string
	}{
		{nil, ""},
		{[]any{map[string]any{"content": "A", "status": "completed"}}, "Plan: 1/1"},
		{[]any{map[string]any{"content": "A", "status": "pending"}, map[string]any{"content": "B", "status": "pending"}}, "Plan: 0/2 — A"},
		{[]any{map[string]any{"content": "A", "status": "pending"}, map[string]any{"content": "B", "status": "in_progress"}}, "Plan: 0/2 — B"},
	} {
		if got := planNoteText(map[string]any{"todos": c.todos}); got != c.want {
			t.Errorf("planNoteText(%v) = %q, want %q", c.todos, got, c.want)
		}
	}
}
