package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The `burrow` CLI is the control API's main client (every agent reaches the app
// through it), and it's shell — so the argument-to-JSON translation gets an
// actual end-to-end run against a stub server rather than a careful reading.

type capturedCall struct {
	Path  string
	Auth  string
	Body  map[string]any
}

func runBurrow(t *testing.T, args ...string) (stdout string, code int, call *capturedCall) {
	t.Helper()

	got := &capturedCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.Path = r.URL.Path
		got.Auth = r.Header.Get("Authorization")
		_ = json.Unmarshal(body, &got.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pty_id":42}`))
	}))
	t.Cleanup(srv.Close)

	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "hook.port"), []byte(port+"\n"+fmt.Sprint(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "control.token"), []byte("tok123"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", append([]string{"bin/burrow"}, args...)...)
	cmd.Env = append(os.Environ(),
		"BURROW_HOME_DIR="+home,
		"BURROW_CWD=/tmp/repo",
		"BURROW_HOOK_PORT=",
	)
	out, err := cmd.CombinedOutput()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code, got
}

func TestCLISendsPositionalAndFlagsAsJSON(t *testing.T) {
	stdout, code, call := runBurrow(t, "spawn", "fix the cache bug", "--agent", "codex", "--model", "claude-sonnet-5")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if call.Path != "/v1/spawn" {
		t.Errorf("path = %q, want /v1/spawn", call.Path)
	}
	if call.Auth != "Bearer tok123" {
		t.Errorf("auth = %q", call.Auth)
	}
	if call.Body["task"] != "fix the cache bug" {
		t.Errorf("positional did not become task: %+v", call.Body)
	}
	if call.Body["agent"] != "codex" || call.Body["model"] != "claude-sonnet-5" {
		t.Errorf("flags lost: %+v", call.Body)
	}
	// Every verb resolves "this repo" from the caller's dir, so it always rides along.
	if call.Body["cwd"] != "/tmp/repo" {
		t.Errorf("cwd = %v, want /tmp/repo", call.Body["cwd"])
	}
	if !strings.Contains(stdout, `"pty_id":42`) {
		t.Errorf("server response not printed: %q", stdout)
	}
}

// Dashes in verb and flag names are what agents type; the API speaks snake_case.
func TestCLINormalisesNamesAndBareFlags(t *testing.T) {
	_, code, call := runBurrow(t, "worktree-remove", "feat/x", "--force")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if call.Path != "/v1/worktree_remove" {
		t.Errorf("path = %q", call.Path)
	}
	if call.Body["branch"] != "feat/x" || call.Body["force"] != "true" {
		t.Errorf("body = %+v", call.Body)
	}

	_, _, call = runBurrow(t, "wait", "res7", "--timeout", "30")
	if call.Path != "/v1/wait_result" {
		t.Errorf("wait should map to wait_result, got %q", call.Path)
	}
	if call.Body["token"] != "res7" || call.Body["timeout"] != "30" {
		t.Errorf("body = %+v", call.Body)
	}
}

// Two positionals in order (send-to-tab takes an id then a message), and quotes
// in the message must survive into valid JSON.
func TestCLIHandlesTwoPositionalsAndQuotes(t *testing.T) {
	_, code, call := runBurrow(t, "send-to-tab", "42", `say "hi" now`)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if call.Body["pty_id"] != "42" || call.Body["text"] != `say "hi" now` {
		t.Errorf("body = %+v", call.Body)
	}
}

func TestCLIRefusesExtraPositional(t *testing.T) {
	stdout, code, _ := runBurrow(t, "agent-status", "nonsense")
	if code == 0 {
		t.Errorf("an unexpected positional should fail, got: %s", stdout)
	}
	if !strings.Contains(stdout, "pass it as --name value") {
		t.Errorf("error should say how to fix it: %q", stdout)
	}
}
