package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The merge must be additive: another tool's hook survives, ours appears once
// no matter how often we run, and an unmerge takes back only ours.
func TestMergeStatusHooksIsAdditiveAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	foreign := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool report"}]}]},"theme":"dark"}`
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := `[ -n "$BURROW_PTY_ID" ] && '/tmp/bin/burrow' hook || true`
	mergeStatusHooks(path, []string{"Stop", "PreToolUse"}, cmd, true)
	mergeStatusHooks(path, []string{"Stop", "PreToolUse"}, cmd, true)

	root := readJSON(t, path)
	if root["theme"] != "dark" {
		t.Errorf("unrelated settings lost: %v", root)
	}
	if root["shiftEnterKeyBindingInstalled"] != true {
		t.Error("shiftEnterKeyBindingInstalled not set")
	}
	hooks := root["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop should hold the foreign hook + exactly one of ours, got %d", len(stop))
	}
	if isBurrowHook(stop[0]) {
		t.Error("foreign hook was replaced instead of appended to")
	}
	if !isBurrowHook(stop[1]) {
		t.Error("our hook missing from Stop")
	}
	if pre := hooks["PreToolUse"].([]any); len(pre) != 1 || !isBurrowHook(pre[0]) {
		t.Errorf("PreToolUse: want exactly our hook, got %v", pre)
	}

	unmergeStatusHooks(path)
	hooks = readJSON(t, path)["hooks"].(map[string]any)
	if stop = hooks["Stop"].([]any); len(stop) != 1 || isBurrowHook(stop[0]) {
		t.Errorf("unmerge should leave only the foreign hook, got %v", stop)
	}
	if pre := hooks["PreToolUse"].([]any); len(pre) != 0 {
		t.Errorf("our PreToolUse hook not removed: %v", pre)
	}
}

// A config we can't parse (JSONC, half-written, whatever) must be left alone,
// never rewritten from an empty object.
func TestMergeStatusHooksSkipsUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	broken := "{ not json // comment\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	mergeStatusHooks(path, []string{"Stop"}, "cmd", false)
	got, _ := os.ReadFile(path)
	if string(got) != broken {
		t.Errorf("unparseable file was rewritten: %q", got)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}
