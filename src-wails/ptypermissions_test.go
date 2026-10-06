package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

func holdAsync(p *permRegistry, ctx context.Context, pty, tool, input string) chan []byte {
	out := make(chan []byte, 1)
	go func() { out <- p.Hold(ctx, pty, tool, json.RawMessage(input), nil) }()
	return out
}

func waitPending(t *testing.T, p *permRegistry, n int) []permRequest {
	t.Helper()
	for i := 0; i < 200; i++ {
		if l := p.List(); len(l) == n {
			return l
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d pending, have %d", n, len(p.List()))
	return nil
}

func recv(t *testing.T, ch chan []byte) []byte {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("hold did not release")
		return nil
	}
}

func TestPermAnswerOutputs(t *testing.T) {
	cases := []struct{ behavior, want string }{
		{"allow", `"decision":{"behavior":"allow"}`},
		{"deny", `"decision":{"behavior":"deny","interrupt":false,"message":"no"}`},
		{"always", `"destination":"session"`},
	}
	for _, c := range cases {
		p := newPermRegistry(nil)
		ch := holdAsync(p, context.Background(), "3", "Bash", `{"command":"npm test"}`)
		req := waitPending(t, p, 1)[0]
		if err := p.Answer(req.ID, c.behavior, "no"); err != nil {
			t.Fatal(err)
		}
		got := string(recv(t, ch))
		if !strings.Contains(got, c.want) || !strings.Contains(got, `"hookEventName":"PermissionRequest"`) {
			t.Errorf("%s: %s", c.behavior, got)
		}
		if c.behavior == "always" && !strings.Contains(got, `"ruleContent":"npm test"`) {
			t.Errorf("always rule lacks the exact command: %s", got)
		}
		waitPending(t, p, 0)
	}
}

func TestPermUnknownAnswerNeverAllows(t *testing.T) {
	p := newPermRegistry(nil)
	ch := holdAsync(p, context.Background(), "3", "Bash", `{"command":"ls"}`)
	req := waitPending(t, p, 1)[0]
	if err := p.Answer(req.ID, "yolo", ""); err == nil {
		t.Fatal("unknown behavior accepted")
	}
	select {
	case b := <-ch:
		t.Fatalf("released with %s", b)
	default:
	}
	if err := p.Answer("3:missing", "allow", ""); err == nil {
		t.Fatal("answering a closed request must fail")
	}
}

func TestPermAlwaysReusesSuggestionAsSession(t *testing.T) {
	req := &permRequest{ToolName: "Bash", suggestions: json.RawMessage(
		`[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"git:*"}],"behavior":"allow","destination":"localSettings"}]`)}
	out, _ := permOutput(req, "always", "")
	if !strings.Contains(string(out), `"ruleContent":"git:*"`) || !strings.Contains(string(out), `"destination":"session"`) ||
		strings.Contains(string(out), "localSettings") {
		t.Fatal(string(out))
	}
}

func TestPermDisconnectClosesWithoutDecision(t *testing.T) {
	p := newPermRegistry(nil)
	ctx, cancel := context.WithCancel(context.Background())
	ch := holdAsync(p, ctx, "3", "Bash", `{"command":"ls"}`)
	waitPending(t, p, 1)
	cancel()
	if b := recv(t, ch); b != nil {
		t.Fatalf("disconnect produced a decision: %s", b)
	}
	waitPending(t, p, 0)
}

func TestPermIdenticalRequestsShareOneCard(t *testing.T) {
	p := newPermRegistry(nil)
	a := holdAsync(p, context.Background(), "3", "Bash", `{"command":"ls"}`)
	b := holdAsync(p, context.Background(), "3", "Bash", `{"command":"ls"}`)
	c := holdAsync(p, context.Background(), "3", "Bash", `{"command":"pwd"}`)
	d := holdAsync(p, context.Background(), "4", "Bash", `{"command":"ls"}`)
	time.Sleep(50 * time.Millisecond)
	list := waitPending(t, p, 3)
	if err := p.Answer(permKey("3", "Bash", json.RawMessage(`{"command":"ls"}`)), "allow", ""); err != nil {
		t.Fatal(err)
	}
	_ = list
	if recv(t, a) == nil || recv(t, b) == nil {
		t.Fatal("both identical hooks must get the answer")
	}
	_, _ = c, d
}

func TestPermClosesWhenTurnEndsEvenIfApprovalNeverObserved(t *testing.T) {
	p := newPermRegistry(nil)
	ch := holdAsync(p, context.Background(), "3", "Bash", `{"command":"ls"}`)
	waitPending(t, p, 1)
	p.phaseChanged("pty:3", agentphase.Phase{State: agentphase.Done})
	if b := recv(t, ch); b != nil {
		t.Fatalf("decision: %s", b)
	}
}

func TestPermClosesWhenPhaseLeavesWaitingApproval(t *testing.T) {
	p := newPermRegistry(nil)
	ch := holdAsync(p, context.Background(), "3", "Bash", `{"command":"ls"}`)
	waitPending(t, p, 1)

	// A stray running event before waiting_approval was ever seen must not close it.
	p.phaseChanged("pty:3", agentphase.Phase{State: agentphase.Running})
	p.phaseChanged("chat:3", agentphase.Phase{State: agentphase.Idle})
	if len(p.List()) != 1 {
		t.Fatal("closed before the approval phase was observed")
	}
	p.phaseChanged("pty:3", agentphase.Phase{State: agentphase.WaitingApproval})
	p.phaseChanged("pty:4", agentphase.Phase{State: agentphase.Running}) // other tab
	if len(p.List()) != 1 {
		t.Fatal("another tab's phase closed it")
	}
	p.phaseChanged("pty:3", agentphase.Phase{State: agentphase.Running})
	if b := recv(t, ch); b != nil {
		t.Fatalf("phase close produced a decision: %s", b)
	}
	waitPending(t, p, 0)
}

// --- HTTP handler + CLI ---------------------------------------------------------

func TestPermissionHandlerSkipsUnansweredTools(t *testing.T) {
	app := &App{controlToken: "tok", perms: newPermRegistry(nil)}
	for _, tool := range []string{"AskUserQuestion", "ExitPlanMode"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/permission_request?pty_id=3", strings.NewReader(`{"tool_name":"`+tool+`","tool_input":{}}`))
		req.Header.Set("Authorization", "Bearer tok")
		app.handlePermissionRequest(rr, req) // would block forever if it parked
		if rr.Code != http.StatusNoContent || len(app.perms.List()) != 0 {
			t.Errorf("%s: code %d", tool, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	app.handlePermissionRequest(rr, httptest.NewRequest("POST", "/v1/permission_request?pty_id=3", strings.NewReader(`{}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rr.Code)
	}
}

func runApprove(t *testing.T, ptyID, stdin string, handler http.HandlerFunc) (string, int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "hook.port"), []byte(port+"\n1"), 0o644)
	_ = os.WriteFile(filepath.Join(home, "control.token"), []byte("tok123"), 0o600)
	cmd := exec.Command("sh", "bin/burrow", "approve")
	cmd.Env = append(os.Environ(), "BURROW_HOME_DIR="+home, "BURROW_HOOK_PORT=", "BURROW_PTY_ID="+ptyID)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestApproveCLI(t *testing.T) {
	const bash = `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`
	hit := 0
	var gotPath, gotAuth, gotBody string
	answer := func(code int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hit++
			gotPath, gotAuth = r.URL.RequestURI(), r.Header.Get("Authorization")
			b := make([]byte, 4096)
			n, _ := r.Body.Read(b)
			gotBody = string(b[:n])
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		}
	}

	const verdict = `{"hookSpecificOutput":{"decision":{"behavior":"allow"}}}`
	if out, code := runApprove(t, "7", bash, answer(200, verdict)); out != verdict || code != 0 {
		t.Errorf("verbatim: %q %d", out, code)
	}
	if gotPath != "/v1/permission_request?pty_id=7" || gotAuth != "Bearer tok123" || gotBody != bash {
		t.Errorf("request: %s %s %s", gotPath, gotAuth, gotBody)
	}
	// 204, 500, 401: no decision, exit 0, never a made-up allow.
	for _, c := range []int{204, 500, 401} {
		if out, code := runApprove(t, "7", bash, answer(c, `{"error":"x"}`)); out != "" || code != 0 {
			t.Errorf("%d: %q %d", c, out, code)
		}
	}
	// Server gone.
	if out, code := runApprove(t, "7", bash, func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		c.Close()
	}); out != "" || code != 0 {
		t.Errorf("dropped: %q %d", out, code)
	}

	hit = 0
	for name, c := range map[string]struct{ pty, in string }{
		"outside burrow": {"", bash},
		"question":       {"7", `{"tool_name":"AskUserQuestion","tool_input":{}}`},
		"plan":           {"7", `{"tool_name":"ExitPlanMode","tool_input":{}}`},
		"garbage":        {"7", `not json`},
	} {
		if out, code := runApprove(t, c.pty, c.in, answer(200, verdict)); out != "" || code != 0 {
			t.Errorf("%s: %q %d", name, out, code)
		}
	}
	if hit != 0 {
		t.Errorf("skipped cases still called the app %d times", hit)
	}
}
