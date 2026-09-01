package main

import (
	"context"
	"encoding/json"
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
	resp, _ := http.DefaultClient.Do(req)
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

// The UI bridge is request/response: emit, block, deliver the frontend's ack to
// the right waiter. Several actions can be outstanding at once (a Manager
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
