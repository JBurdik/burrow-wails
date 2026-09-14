package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Real `model/list` payload shape from codex-cli 0.149.1 (trimmed to two models).
const codexModelList = `{"data":[
 {"id":"gpt-5.6-sol","model":"gpt-5.6-sol","displayName":"GPT-5.6-Sol","description":"Latest frontier agentic coding model.","hidden":false,"isDefault":true,
  "defaultReasoningEffort":"low",
  "supportedReasoningEfforts":[{"reasoningEffort":"low","description":"Fast"},{"reasoningEffort":"high","description":"Deep"}]},
 {"id":"gpt-5.6-luna","model":"gpt-5.6-luna","displayName":"GPT-5.6-Luna","hidden":false,"isDefault":false,
  "defaultReasoningEffort":"medium","supportedReasoningEfforts":["medium"]},
 {"id":"hidden-one","displayName":"Hidden","hidden":true}
]}`

func TestCodexConfigOptions(t *testing.T) {
	var result map[string]any
	if err := json.Unmarshal([]byte(codexModelList), &result); err != nil {
		t.Fatal(err)
	}
	entries, _ := result["data"].([]any)
	opts := codexConfigOptions(entries)
	if len(opts) != 2 {
		t.Fatalf("want model+effort options, got %d", len(opts))
	}
	model := mapOf(opts[0])
	if model["id"] != "model" || model["currentValue"] != "gpt-5.6-sol" {
		t.Fatalf("bad model option: %v", model)
	}
	list, _ := model["options"].([]any)
	if len(list) != 2 { // hidden model dropped
		t.Fatalf("want 2 visible models, got %d", len(list))
	}
	if mapOf(list[0])["name"] != "GPT-5.6-Sol" {
		t.Fatalf("bad model entry: %v", list[0])
	}
	effort := mapOf(opts[1])
	if effort["currentValue"] != "low" {
		t.Fatalf("bad effort default: %v", effort)
	}
	efforts, _ := effort["options"].([]any)
	if len(efforts) != 2 || mapOf(efforts[1])["value"] != "high" {
		t.Fatalf("bad effort options: %v", efforts)
	}
}

func TestCodexConfigOptionsEmpty(t *testing.T) {
	if got := codexConfigOptions(nil); len(got) != 0 {
		t.Fatalf("want no options for empty model/list, got %v", got)
	}
}

func TestCodexTurnTerminalFailure(t *testing.T) {
	if got := codexTurnTerminalFailure(map[string]any{
		"turn": map[string]any{"status": "completed"},
	}); got != "" {
		t.Fatalf("completed turn should not show an error, got %q", got)
	}
	if got := codexTurnTerminalFailure(map[string]any{
		"turn": map[string]any{"status": "failed", "error": map[string]any{"message": "rate limited"}},
	}); got != "rate limited" {
		t.Fatalf("failed turn error = %q, want rate limited", got)
	}
	if got := codexTurnTerminalFailure(map[string]any{
		"turn": map[string]any{"status": "failed"},
	}); got != "The Codex app-server ended the turn without an error message." {
		t.Fatalf("failed turn fallback = %q", got)
	}
}

func TestCodexToolCall(t *testing.T) {
	id, title, input, ok := codexToolCall(map[string]any{
		"id": "cmd-1", "type": "commandExecution", "command": "rg TODO", "cwd": "/repo",
	})
	if !ok || id != "cmd-1" || title != "Run: rg TODO" || input["cwd"] != "/repo" {
		t.Fatalf("bad command tool call: id=%q title=%q input=%v ok=%t", id, title, input, ok)
	}

	_, title, input, ok = codexToolCall(map[string]any{
		"id": "mcp-1", "type": "mcpToolCall", "server": "github", "tool": "search", "arguments": map[string]any{"q": "bug"},
	})
	if !ok || title != "github: search" || mapOf(input["arguments"])["q"] != "bug" {
		t.Fatalf("bad MCP tool call: title=%q input=%v ok=%t", title, input, ok)
	}

	if _, _, _, ok := codexToolCall(map[string]any{"id": "msg-1", "type": "agentMessage"}); ok {
		t.Fatal("agent messages must not render as tool calls")
	}
}

func TestCodexToolOutputAndFailure(t *testing.T) {
	item := map[string]any{
		"type": "commandExecution", "status": "failed", "aggregatedOutput": "permission denied",
	}
	if !codexToolFailed(item) || codexToolOutput(item) != "permission denied" {
		t.Fatalf("bad failed tool conversion: failed=%t output=%q", codexToolFailed(item), codexToolOutput(item))
	}
}

func TestCodexRPCErrorMessage(t *testing.T) {
	if got := codexRPCErrorMessage(map[string]any{"error": map[string]any{"message": "invalid params"}}); got != "invalid params" {
		t.Fatalf("error message = %q", got)
	}
	if got := codexRPCErrorMessage(map[string]any{"result": map[string]any{}}); got != "" {
		t.Fatalf("successful response must not be an error: %q", got)
	}
}

func TestRejectUnsupportedCodexRequestRepliesWithJSONRPCError(t *testing.T) {
	var stdin bytes.Buffer
	sess := &acpSession{stdin: nopWriteCloser{Writer: &stdin}}
	(&App{}).rejectUnsupportedCodexRequest(sess, map[string]any{"id": float64(17)}, "item/tool/call")

	var reply map[string]any
	if err := json.Unmarshal(stdin.Bytes(), &reply); err != nil {
		t.Fatalf("invalid JSON-RPC response: %v", err)
	}
	if reply["id"] != float64(17) || mapOf(reply["error"])["code"] != float64(-32601) {
		t.Fatalf("unexpected unsupported-request response: %#v", reply)
	}
}

func TestCodexUserInputResponseUsesStructuredAnswers(t *testing.T) {
	var stdin bytes.Buffer
	sess := &acpSession{stdin: nopWriteCloser{Writer: &stdin}, proto: protoCodexAppServer}
	app := &App{acpSessions: &acpRegistry{live: map[string]*acpSession{"chat": sess}}}
	if err := app.AcpRespondUserInput("chat", 18, map[string][]string{"language": {"Go"}}); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := json.Unmarshal(stdin.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	answers := mapOf(mapOf(reply["result"])["answers"])
	values, _ := mapOf(answers["language"])["answers"].([]any)
	if reply["id"] != float64(18) || len(values) != 1 || values[0] != "Go" {
		t.Fatalf("unexpected Codex user-input response: %#v", reply)
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestCodexModeSettings(t *testing.T) {
	cases := map[string]struct {
		approval string
		sandbox  string
		reviewer string
	}{
		"default":           {"untrusted", "readOnly", "user"},
		"acceptEdits":       {"on-request", "workspaceWrite", "user"},
		"auto":              {"on-request", "workspaceWrite", "auto_review"},
		"plan":              {"untrusted", "readOnly", "user"},
		"bypassPermissions": {"never", "dangerFullAccess", "user"},
	}
	for mode, want := range cases {
		approval, sandbox, reviewer, ok := codexModeSettings(mode)
		if !ok || approval != want.approval || sandbox != want.sandbox || reviewer != want.reviewer {
			t.Fatalf("mode %q: got (%q, %q, %q, %t), want (%q, %q, %q, true)", mode, approval, sandbox, reviewer, ok, want.approval, want.sandbox, want.reviewer)
		}
	}
	if _, _, _, ok := codexModeSettings("not-a-mode"); ok {
		t.Fatal("unknown mode must not silently change Codex settings")
	}
}

// Live smoke test against the installed Codex CLI: proves the probe (spawn →
// initialize → model/list) really returns models. Skipped when codex is absent.
func TestCodexListModelsLive(t *testing.T) {
	if resolveAgentBin("codex", ".") == "" {
		t.Skip("codex not installed")
	}
	models, err := (&App{}).CodexListModels(".")
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("probe returned no models")
	}
	for _, m := range models {
		if m.ID == "" || m.Label == "" {
			t.Fatalf("incomplete model entry: %+v", m)
		}
	}
	t.Logf("codex models: %+v", models)
}

// failingWriteCloser simulates a registered-but-dead ACP/Codex pipe: the
// session is still in the registry (acpReg().get succeeds), but the process
// on the other end is gone, so every Write fails the way a broken stdin pipe
// would.
type failingWriteCloser struct{}

func (failingWriteCloser) Write([]byte) (int, error) { return 0, fmt.Errorf("broken pipe") }
func (failingWriteCloser) Close() error              { return nil }

// TestAcpSendDoesNotRecordAnUndeliveredMessage covers the same data-loss
// shape fixed in ClaudeSend (IMPORTANT 3), for the plain-ACP branch:
// acpReg().get(id) only proves the session is REGISTERED, not that its pipe
// is alive, so AcpSend used to call emitChatLine BEFORE sess.write — a
// registered-but-broken session recorded a user turn nothing ever received.
func TestAcpSendDoesNotRecordAnUndeliveredMessage(t *testing.T) {
	a := newTestApp(t)
	const chatID = "55"
	sess := &acpSession{stdin: failingWriteCloser{}, proto: protoACP, sessionID: "sess-1"}
	a.acpReg().put(chatID, sess)

	if _, err := a.AcpSend(chatID, "are you there?", nil); err == nil {
		t.Fatal("want an error sending on a broken pipe, got nil")
	} else if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error doesn't name the dead process clearly: %q", err)
	}

	msgs := loadFolded(t, a, 55)
	if len(msgs) != 0 {
		t.Fatalf("want no transcript rows for an undelivered send, got %d: %v", len(msgs), msgs)
	}
}

// Same coverage for the Codex-app-server branch, which takes a different
// early-return path (turn/start, finishCodexTurn on failure) than plain ACP.
func TestCodexAppServerSendDoesNotRecordAnUndeliveredMessage(t *testing.T) {
	a := newTestApp(t)
	const chatID = "56"
	sess := &acpSession{stdin: failingWriteCloser{}, proto: protoCodexAppServer, sessionID: "thread-1"}
	a.acpReg().put(chatID, sess)

	if _, err := a.AcpSend(chatID, "are you there?", nil); err == nil {
		t.Fatal("want an error sending on a broken pipe, got nil")
	} else if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error doesn't name the dead process clearly: %q", err)
	}

	msgs := loadFolded(t, a, 56)
	if len(msgs) != 0 {
		t.Fatalf("want no transcript rows for an undelivered send, got %d: %v", len(msgs), msgs)
	}
}

// The ordinary path must still work: a live pipe records the user line and
// AcpSend returns no error.
func TestAcpSendRecordsOnASuccessfulWrite(t *testing.T) {
	a := newTestApp(t)
	const chatID = "57"
	r, w := io.Pipe()
	defer r.Close()
	go io.Copy(io.Discard, r) // drain so writes don't block
	sess := &acpSession{stdin: w, proto: protoACP, sessionID: "sess-2"}
	a.acpReg().put(chatID, sess)

	if _, err := a.AcpSend(chatID, "hello there", nil); err != nil {
		t.Fatalf("AcpSend: %v", err)
	}

	msgs := loadFolded(t, a, 57)
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "user" {
		t.Fatalf("want the user line recorded on a successful write, got %v", msgs)
	}
}
