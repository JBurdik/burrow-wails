package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"burrow/internal/control"
	"burrow/internal/ctlauth"
)

// testToken is hex, like every real control.token (bin/burrow's HMAC reads hex keys).
const testToken = "abc123"

// signedStub is a core stand-in: the real challenge + controlAuth in front of h.
func signedStub(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	app := &App{controlToken: testToken}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/_challenge", app.handleControlChallenge)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := app.controlAuth(w, r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func srvPort(t *testing.T, raw string) int {
	u, _ := url.Parse(raw)
	n, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func signedPost(t *testing.T, c ctlauth.Client, path, body string) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", c.Port, path), strings.NewReader(body))
	return c.Do(req)
}

func TestControlChallengeRoundTrip(t *testing.T) {
	app := &App{controlToken: testToken, control: control.New(control.Deps{})}
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()
	port := srvPort(t, srv.URL)

	resp, err := signedPost(t, ctlauth.Client{Port: port, Key: testToken}, "/v1/_verbs", "")
	if err != nil {
		t.Fatalf("signed call: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed call: %d", resp.StatusCode)
	}

	// Wrong key: refused, and the client doesn't take the reply.
	if _, err := signedPost(t, ctlauth.Client{Port: port, Key: "deadbeef"}, "/v1/_verbs", ""); err == nil {
		t.Error("a wrong key passed")
	}

	// A nonce is single use, and a proof is bound to the port.
	challenge := func() string {
		r, _ := http.Post(srv.URL+"/v1/_challenge", "", nil)
		var ch struct{ Nonce string }
		_ = json.NewDecoder(r.Body).Decode(&ch)
		r.Body.Close()
		return ch.Nonce
	}
	call := func(h string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/_verbs", nil)
		req.Header.Set("Authorization", h)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	sn, cn := challenge(), strings.Repeat("c", 64)
	h := "Burrow-Proof " + sn + " " + cn + " " + ctlauth.Proof(testToken, "client", sn, cn, port)
	if call(h) != 200 || call(h) != 401 {
		t.Error("a replayed proof was accepted (or the first one refused)")
	}
	sn = challenge()
	if call("Burrow-Proof "+sn+" "+cn+" "+ctlauth.Proof(testToken, "client", sn, cn, port+1)) != 401 {
		t.Error("a proof for another port was accepted")
	}
	// The server's own proof can't be played back as a client's.
	sn = challenge()
	if call("Burrow-Proof "+sn+" "+cn+" "+ctlauth.Proof(testToken, "server", sn, cn, port)) != 401 {
		t.Error("a server proof passed as a client proof")
	}
	// Deprecated Bearer still works for one release.
	if call("Bearer "+testToken) != 200 {
		t.Error("legacy Bearer refused")
	}
}

// A chat's key is its identity: the hint must agree, and another chat's key
// can't be passed off as this one's.
func TestCallerKeyBindsIdentity(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	a.controlToken = testToken
	a.control = control.New(control.Deps{DB: a.db})
	srv := httptest.NewServer(controlMux(a))
	defer srv.Close()
	port := srvPort(t, srv.URL)

	key5 := a.callerKeyEnv("chat", 5)
	if _, err := signedPost(t, ctlauth.Client{Port: port, Key: key5, Caller: "chat:6"}, "/v1/_verbs", ""); err == nil {
		t.Error("chat 5's key passed as chat 6")
	}
	resp, err := signedPost(t, ctlauth.Client{Port: port, Key: key5, Caller: "chat:5"}, "/v1/schedule_list", `{"parent_chat_id":"6"}`)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "names another caller") {
		t.Errorf("mismatched hint: %d %s", resp.StatusCode, body)
	}

	p := control.Params{"parent_chat_id": "5"}
	if bindCaller(ctlCaller{Kind: "chat", ID: 5}, p) != "" || p.Int("parent_chat_id") != 5 {
		t.Errorf("matching hint refused: %v", p)
	}
	p = control.Params{}
	if bindCaller(ctlCaller{Kind: "pty", ID: 3}, p) != "" || p.Int("caller_pty_id") != 3 {
		t.Errorf("pty key not bound: %v", p)
	}
	if bindCaller(ctlCaller{Kind: "pty", ID: 3}, control.Params{"parent_chat_id": "9"}) == "" {
		t.Error("a tab key claiming a chat passed")
	}
	p = control.Params{"parent_chat_id": "9"}
	if bindCaller(ctlCaller{}, p) != "" || p.Int("parent_chat_id") != 9 {
		t.Error("the master key's hints must stay as sent")
	}
}

// End to end over HTTP: a default-mode chat can't use its key to spawn a
// full-access sub-agent, and the refusal points at the user.
func TestChatKeyCannotEscalateOverHTTP(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	limited, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "limited"})
	free, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "free"})
	a.db.Exec(`UPDATE chats SET permission_mode = 'full-access' WHERE id = ?`, free.ID)
	a.controlToken = testToken
	a.control = control.New(control.Deps{DB: a.db, UI: spawnStubUI{}})
	srv := httptest.NewServer(controlMux(a))
	defer srv.Close()
	c := ctlauth.Client{Port: srvPort(t, srv.URL), Key: a.callerKeyEnv("chat", limited.ID), Caller: fmt.Sprintf("chat:%d", limited.ID)}

	for path, body := range map[string]string{
		"/v1/spawn":     `{"task":"x","permission_mode":"full-access"}`,
		"/v1/chat_send": fmt.Sprintf(`{"chat_id":%d,"text":"hi"}`, free.ID),
	} {
		resp, err := signedPost(t, c, path, body)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(out), "Ask the user") {
			t.Errorf("%s: %d %s", path, resp.StatusCode, out)
		}
	}
}

func TestCapabilityProblem(t *testing.T) {
	me := capChat{ID: 1, Mode: "default"}
	child := &capChat{ID: 2, Parent: 1, Mode: "default"}
	freeChild := &capChat{ID: 3, Parent: 1, Mode: "full-access"}
	other := &capChat{ID: 4, Mode: "default"}
	othersChild := &capChat{ID: 5, Parent: 4, Mode: "plan"}
	cases := []struct {
		name string
		call capCall
		ok   bool
	}{
		{"spawn narrower", capCall{Verb: "spawn", Caller: me, Mode: "plan"}, true},
		{"spawn same", capCall{Verb: "spawn", Caller: me, Mode: "default"}, true},
		{"spawn freer", capCall{Verb: "spawn", Caller: me, Mode: "acceptEdits"}, false},
		{"spawn full", capCall{Verb: "spawn", Caller: me, Mode: "full-access"}, false},
		{"spawn detached", capCall{Verb: "spawn", Caller: me, Detach: true}, false},
		{"full-access caller unlimited", capCall{Verb: "spawn", Caller: capChat{ID: 1, Mode: "bypassPermissions"}, Mode: "full-access", Detach: true}, true},
		{"send own child", capCall{Verb: "chat_send", Caller: me, Target: child}, true},
		{"send freer child", capCall{Verb: "chat_send", Caller: me, Target: freeChild}, false},
		{"answer freer child", capCall{Verb: "chat_answer", Caller: me, Target: freeChild}, false},
		{"send other top-level", capCall{Verb: "chat_send", Caller: me, Target: other}, true},
		{"send other's sub-agent", capCall{Verb: "chat_send", Caller: me, Target: othersChild}, false},
		{"interrupt other's sub-agent", capCall{Verb: "chat_interrupt", Caller: me, Target: othersChild}, false},
		{"close own child", capCall{Verb: "close_chat", Caller: me, Target: child}, true},
		{"close other top-level", capCall{Verb: "close_chat", Caller: me, Target: other}, false},
		{"read other's sub-agent", capCall{Verb: "chat_read", Caller: me, Target: othersChild}, true},
		{"move self", capCall{Verb: "move_to_worktree", Caller: me, Target: &capChat{ID: 1, Mode: "default"}}, true},
		{"move freer chat", capCall{Verb: "move_to_worktree", Caller: me, Target: &capChat{ID: 9, Mode: "dontAsk"}}, false},
		{"schedule self", capCall{Verb: "schedule", Caller: me}, true},
		{"schedule into freer chat", capCall{Verb: "schedule", Caller: me, Target: &capChat{ID: 9, Mode: "auto"}}, false},
		{"schedule new chats", capCall{Verb: "schedule", Caller: me, NewChat: true}, false},
		{"schedule a check", capCall{Verb: "schedule", Caller: me, Check: true}, false},
		{"run another's new-chat schedule", capCall{Verb: "schedule_run_now", Caller: me, NewChat: true}, false},
		{"plan caller sends default chat", capCall{Verb: "chat_send", Caller: capChat{ID: 1, Mode: "plan"}, Target: other}, false},
	}
	for _, c := range cases {
		got := capabilityProblem(c.call)
		if (got == "") != c.ok {
			t.Errorf("%s: problem %q, want ok=%v", c.name, got, c.ok)
		}
		if got != "" && !strings.Contains(got, "Ask the user") {
			t.Errorf("%s: refusal doesn't point at the user: %q", c.name, got)
		}
	}
}

// The CLI end to end: it signs (never sends the token), and drops a reply
// from a server that can't prove itself.
func TestCLIRefusesAnImpostor(t *testing.T) {
	var gotAuth string
	impostor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/_challenge" {
			fmt.Fprint(w, `{"nonce":"`+strings.Repeat("a", 64)+`"}`)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set(ctlauth.ProofHeader, strings.Repeat("0", 64))
		fmt.Fprint(w, `{"secret":"forged"}`)
	}))
	defer impostor.Close()
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "hook.port"), []byte(strconv.Itoa(srvPort(t, impostor.URL))+"\n1"), 0o644)
	_ = os.WriteFile(filepath.Join(home, "control.token"), []byte(testToken), 0o600)
	cmd := exec.Command("sh", "bin/burrow", "list-workspaces")
	cmd.Env = append(os.Environ(), "BURROW_HOME_DIR="+home, "BURROW_HOOK_PORT=", "BURROW_CALLER_TOKEN=")
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "forged") || !strings.Contains(string(out), "did not prove") {
		t.Errorf("impostor's reply trusted: err=%v out=%s", err, out)
	}
	if !strings.HasPrefix(gotAuth, "Burrow-Proof ") || strings.Contains(gotAuth, testToken) {
		t.Errorf("auth sent = %q", gotAuth)
	}
}

// A chat's CLI signs with its own key and names itself.
func TestCLISignsWithCallerKey(t *testing.T) {
	stdout, code, call := runBurrowWithEnv(t, []string{
		"BURROW_CHAT_ID=5",
		"BURROW_PTY_ID=",
		"BURROW_CALLER_TOKEN=" + ctlauth.CallerKey(testToken, "chat:5"),
	}, "agent-status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if !strings.HasSuffix(call.Auth, " chat:5") {
		t.Errorf("auth = %q", call.Auth)
	}
}

// Every chat runtime (Codex, ACP, OpenAI-compatible) gets its key here; Claude
// in claudechat.go, tabs in CreatePty.
func TestChatEnvCarriesCallerKey(t *testing.T) {
	a := &App{controlToken: testToken}
	env := map[string]string{}
	a.addBurrowEnvForChat(env, "/tmp", 7)
	if env["BURROW_CALLER_TOKEN"] != ctlauth.CallerKey(testToken, "chat:7") || env["BURROW_CHAT_ID"] != "7" {
		t.Errorf("env = %v", env)
	}
}
