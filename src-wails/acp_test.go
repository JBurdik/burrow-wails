package main

import (
	"encoding/json"
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

func TestCodexModeSettings(t *testing.T) {
	cases := map[string]struct {
		approval string
		sandbox  string
	}{
		"default":           {"untrusted", "readOnly"},
		"acceptEdits":       {"on-request", "workspaceWrite"},
		"plan":              {"untrusted", "readOnly"},
		"bypassPermissions": {"never", "dangerFullAccess"},
	}
	for mode, want := range cases {
		approval, sandbox, ok := codexModeSettings(mode)
		if !ok || approval != want.approval || sandbox != want.sandbox {
			t.Fatalf("mode %q: got (%q, %q, %t), want (%q, %q, true)", mode, approval, sandbox, ok, want.approval, want.sandbox)
		}
	}
	if _, _, ok := codexModeSettings("not-a-mode"); ok {
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
