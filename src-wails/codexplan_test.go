package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A Codex `turn/plan/updated` notification (app-server v2,
// TurnPlanUpdatedNotification) must reach the transcript as the same
// TodoWrite row a Claude plan is, through the persisted ACP `plan` line.
func TestCodexPlanUpdateBecomesTodoWriteRow(t *testing.T) {
	var msg map[string]any
	raw := `{"method":"turn/plan/updated","params":{"threadId":"th-1","turnId":"tu-1","explanation":"Ship it",` +
		`"plan":[{"step":"Read code","status":"completed"},{"step":"Write fix","status":"inProgress"},` +
		`{"step":"Test","status":"pending"},{"step":"","status":"pending"}]}}`
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"method": "session/update", "params": map[string]any{
		"update": codexPlanUpdate(mapOf(msg["params"]))}})
	if err != nil {
		t.Fatal(err)
	}
	events := NormalizeAcpLine(string(line))
	if len(events) != 2 || events[0].Type != EvtToolStarted || events[1].Type != EvtToolCompleted {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Name != "TodoWrite" || events[0].ToolCallID != events[1].ToolCallID {
		t.Fatalf("started = %+v", events[0])
	}
	want := map[string]any{
		"explanation": "Ship it",
		"todos": []any{
			map[string]any{"content": "Read code", "status": "completed"},
			map[string]any{"content": "Write fix", "status": "in_progress"},
			map[string]any{"content": "Test", "status": "pending"},
		},
	}
	if !reflect.DeepEqual(events[0].Input, want) {
		t.Fatalf("input = %#v", events[0].Input)
	}
}

// Real line from chat_stream (Codex 0.164 fileChange, translated by
// pumpCodexLine): the input keeps `changes` with kind as an object.
func TestCodexFileChangeToolCallKeepsChanges(t *testing.T) {
	line := `{"method":"session/update","params":{"update":{"input":{"changes":[{"diff":"@@ -18,3 +18,3 @@\n","kind":{"move_path":null,"type":"update"},"path":"/Users/jirkab/code/natalies/compose.yml"}]},"sessionUpdate":"tool_call","title":"Apply file changes","toolCallId":"exec-0e9b6b27-681e-4979-8482-22e52210017e"}}}`
	events := NormalizeAcpLine(line)
	if len(events) != 1 || events[0].Type != EvtToolStarted || events[0].Name != "Apply file changes" {
		t.Fatalf("events = %+v", events)
	}
	changes, _ := events[0].Input["changes"].([]any)
	if len(changes) != 1 || mapOf(mapOf(changes[0])["kind"])["type"] != "update" {
		t.Fatalf("changes = %#v", events[0].Input["changes"])
	}
}

func TestCodexDeclinedToolCountsAsFailed(t *testing.T) {
	if !codexToolFailed(map[string]any{"type": "fileChange", "status": "declined"}) {
		t.Fatal("declined patch must not count as applied")
	}
	if codexToolFailed(map[string]any{"type": "fileChange", "status": "completed"}) {
		t.Fatal("completed patch is not a failure")
	}
}
