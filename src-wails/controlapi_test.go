package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"burrow/internal/control"
)

// The loopback control API is reachable by every process on this machine and
// `spawn` starts programs, so the token is the whole security boundary.
func TestControlAPIRequiresToken(t *testing.T) {
	app := &App{controlToken: "secret"}
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()

	for name, header := range map[string]string{
		"no header":    "",
		"wrong token":  "Bearer nope",
		"bare token":   "secret",
		"empty bearer": "Bearer ",
	} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/list_workspaces", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", name, resp.StatusCode)
		}
	}
}

// A token that failed to persist must fail closed, not open.
func TestControlAPIFailsClosedWithoutToken(t *testing.T) {
	app := &App{controlToken: ""}
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/list_workspaces", nil)
	req.Header.Set("Authorization", "Bearer ")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", resp.StatusCode)
	}
}

func TestControlAPIUnknownVerbIs404(t *testing.T) {
	app := &App{controlToken: "t"}
	app.control = control.New(control.Deps{})
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()

	resp := post(t, srv.URL+"/v1/no_such_verb", "t", "{}")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %d, want 404", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !strings.Contains(body["error"], "unknown verb") {
		t.Errorf("error = %q", body["error"])
	}
}

// /v1/_verbs is what burrow-mcp turns into tool schemas and `burrow help` prints.
func TestControlAPIServesTheRegistry(t *testing.T) {
	app := &App{controlToken: "t"}
	app.control = control.New(control.Deps{})
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()

	resp := post(t, srv.URL+"/v1/_verbs", "t", "")
	defer resp.Body.Close()
	var verbs []ControlVerb
	if err := json.NewDecoder(resp.Body).Decode(&verbs); err != nil {
		t.Fatal(err)
	}
	if len(verbs) < 20 {
		t.Fatalf("got %d verbs", len(verbs))
	}
	for _, v := range verbs {
		if v.Name == "spawn" {
			if len(v.Args) == 0 || v.Args[0].Name != "task" || !v.Args[0].Required {
				t.Errorf("spawn's schema lost its required task arg: %+v", v.Args)
			}
			return
		}
	}
	t.Error("spawn missing from the registry")
}

// The installed SKILL.md is intentionally only a stable stub. This endpoint is
// the live replacement, so this exhaustiveness test makes a new registry verb
// impossible to add without appearing in `burrow skills get burrow`.
func TestSkillsGetBurrowNamesEveryControlVerb(t *testing.T) {
	app := &App{controlToken: "t", control: control.New(control.Deps{})}
	srv := httptest.NewServer(controlMux(app))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/skills/burrow", nil)
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	guide := string(body)
	for _, verb := range app.control.Verbs() {
		command := "`burrow " + strings.ReplaceAll(verb.Name, "_", "-") + "`"
		if !strings.Contains(guide, command) {
			t.Errorf("live guide omits %s", command)
		}
	}

	for _, path := range []string{"/v1/skills/burrow?references", "/v1/skills/burrow?reference=worktrees"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer t")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: got %d, want 200", path, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("%s: empty response", path)
		}
	}
}

// The UI bridge is request/response: emit, block, deliver the frontend's ack to
// the right waiter. Several actions can be outstanding at once (a thread
// spawning three agents), so ids must not cross.
func TestUIBridgeDeliversAcksToTheRightCaller(t *testing.T) {
	app := &App{}
	bridge := newUIBridge(app)
	app.ui = bridge

	var mu sync.Mutex
	seen := map[string]string{} // action -> request id
	bridge.emit = func(_ string, payload any) {
		p := payload.(map[string]any)
		mu.Lock()
		seen[p["action"].(string)] = p["id"].(string)
		mu.Unlock()
	}

	results := make(chan string, 2)
	for _, action := range []string{"first", "second"} {
		go func(action string) {
			raw, err := bridge.Do(context.Background(), action, nil)
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- string(raw)
		}(action)
	}

	// Ack in the opposite order to prove the ids are honoured, not the arrival order.
	ids := waitForIDs(t, &mu, seen, "first", "second")
	app.AckControlAction(ids["second"], `{"who":"second"}`, "")
	app.AckControlAction(ids["first"], `{"who":"first"}`, "")

	got := []string{<-results, <-results}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, `"who":"first"`) || !strings.Contains(joined, `"who":"second"`) {
		t.Errorf("acks got crossed: %v", got)
	}
}

func TestUIBridgeReportsFrontendErrors(t *testing.T) {
	app := &App{}
	bridge := newUIBridge(app)
	app.ui = bridge

	ids := make(chan string, 1)
	bridge.emit = func(_ string, payload any) { ids <- payload.(map[string]any)["id"].(string) }

	done := make(chan error, 1)
	go func() {
		_, err := bridge.Do(context.Background(), "focus_tab", nil)
		done <- err
	}()
	app.AckControlAction(<-ids, "", "no open tab with pty id 9")

	err := <-done
	if err == nil || err.Error() != "no open tab with pty id 9" {
		t.Errorf("want the frontend's own message, got %v", err)
	}
}

// A UI that never answers (window closed, JS exception) must surface as an
// error the agent can read, not a hung call.
func TestUIBridgeTimesOutWhenTheUINeverAnswers(t *testing.T) {
	app := &App{}
	bridge := newUIBridge(app)
	bridge.emit = func(string, any) {}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := bridge.Do(ctx, "spawn", nil); err == nil {
		t.Error("want an error once the caller's context expires")
	}
}

// controlMux is the test's stand-in for the hook server's mux.
func controlMux(app *App) http.Handler {
	mux := http.NewServeMux()
	app.registerControlRoutes(mux)
	return mux
}

func post(t *testing.T, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A chat agent has no BURROW_PTY_ID (a chat is not a tab), so the chat id is
// the only thing that can tell the control API which thread a spawn came from.
// burrow-mcp is a child of the CLI process and inherits this, so both doors
// agree without a second mechanism.
func TestAddBurrowEnvCarriesChatID(t *testing.T) {
	a := &App{}
	env := map[string]string{}
	a.addBurrowEnvForChat(env, t.TempDir(), 42)
	if env["BURROW_CHAT_ID"] != "42" {
		t.Fatalf("BURROW_CHAT_ID = %q, want \"42\"", env["BURROW_CHAT_ID"])
	}
}

// A chat id of 0 means "not a chat" — exporting it would make every tab look
// like a child of chat 0.
func TestAddBurrowEnvOmitsZeroChatID(t *testing.T) {
	a := &App{}
	env := map[string]string{}
	// addBurrowEnv itself never touches BURROW_CHAT_ID at all — that's
	// addBurrowEnvForChat's job (see its comment: zero is left unset rather
	// than exported as "0"). Calling addBurrowEnv here tested a tautology and
	// would keep passing even if addBurrowEnvForChat started exporting "0".
	a.addBurrowEnvForChat(env, t.TempDir(), 0)
	if _, ok := env["BURROW_CHAT_ID"]; ok {
		t.Fatal("BURROW_CHAT_ID was exported with no chat")
	}
}

func waitForIDs(t *testing.T, mu *sync.Mutex, seen map[string]string, actions ...string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		out := map[string]string{}
		for _, a := range actions {
			if id, ok := seen[a]; ok {
				out[a] = id
			}
		}
		mu.Unlock()
		if len(out) == len(actions) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("only saw %v", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The guard has to be server-side: a prompt can say anything, so the app is
// what decides whether the caller is already somebody's sub-agent.
func TestCallerIsSubagentIsServerDerived(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	child, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})

	if a.chatIsSubagent(parent.ID) {
		t.Error("a top-level chat was called a sub-agent")
	}
	if !a.chatIsSubagent(child.ID) {
		t.Error("a child chat was not recognised as a sub-agent")
	}
}

// spawnStubUI answers /v1/spawn requests that make it past the sub-agent
// guard, so a positive-control POST has something to succeed against instead
// of erroring for an unrelated reason ("no UI attached").
type spawnStubUI struct{}

func (spawnStubUI) Do(ctx context.Context, action string, args map[string]any) (json.RawMessage, error) {
	return json.Marshal(control.SpawnResult{ChatID: 99, Target: "chat"})
}

// The unforgeability of caller_is_subagent is the whole point of deriving it
// server-side rather than trusting the request body — so this has to go
// through the actual HTTP handler, not just Core.Call or chatIsSubagent in
// isolation. A weakened handler (e.g. "default caller_is_subagent to the
// client's value when present") would still pass every other test in this
// file while a forged "caller_is_subagent": false defeated the depth guard.
func TestSpawnCallerIsSubagentCannotBeForgedOverHTTP(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()
	parent, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "parent"})
	child, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "child", ParentChatID: parent.ID})

	a.controlToken = "t"
	a.control = control.New(control.Deps{DB: a.db, UI: spawnStubUI{}})
	srv := httptest.NewServer(controlMux(a))
	defer srv.Close()

	// The caller IS a sub-agent (its own parent_chat_id is non-zero), but the
	// request body lies and says caller_is_subagent: false. The server must
	// still refuse, because it derives the flag itself from parent_chat_id
	// rather than trusting the one in the body.
	forged := fmt.Sprintf(`{"task":"do more work","parent_chat_id":%d,"caller_is_subagent":false}`, child.ID)
	resp := post(t, srv.URL+"/v1/spawn", "t", forged)
	defer resp.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusOK || !strings.Contains(out["error"], "sub-agent cannot spawn sub-agents") {
		t.Fatalf("forged caller_is_subagent: status=%d body=%v, want the sub-agent guard", resp.StatusCode, out)
	}

	// Positive control: a genuinely top-level chat is not refused with that
	// message. Without this, a handler that refuses every /v1/spawn call would
	// also pass the assertion above for the wrong reason.
	legit := fmt.Sprintf(`{"task":"do more work","parent_chat_id":%d}`, parent.ID)
	resp2 := post(t, srv.URL+"/v1/spawn", "t", legit)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		var errOut map[string]string
		_ = json.NewDecoder(resp2.Body).Decode(&errOut)
		t.Fatalf("top-level spawn: status=%d body=%v, want success", resp2.StatusCode, errOut)
	}
}
