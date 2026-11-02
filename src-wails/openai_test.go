package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func openAITestConfig(server string) OpenAIConfig {
	return OpenAIConfig{BaseURL: server + "/v1", Model: "local/coder", ContextWindow: 32768, MaxOutputTokens: 4096}
}
func openAITestStream(w http.ResponseWriter, chunks ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func openAITestText(text string) any {
	return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}, "finish_reason": "stop"}}}
}
func openAITestTool(name, args string) any {
	return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}}
}
func openAITestWait(t *testing.T, s *openAISession) {
	t.Helper()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not settle")
	}
}

func TestOpenAIModelDiscoveryAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected discovery: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, `{"data":[{"id":"coder"},{"id":"coder"},{"id":"other"}]}`)
	}))
	defer server.Close()
	cfg := openAITestConfig(server.URL)
	cfg.APIKey = "test-key"
	models, err := (&App{}).OpenAIListModels(cfg)
	if err != nil || len(models) != 2 || models[0].ID != "coder" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	for _, base := range []string{"file:///tmp/models", "http://user:secret@localhost/v1", "http://localhost/v1?key=secret", "http://localhost/v1#secret"} {
		cfg.BaseURL = base
		if _, err := normalizeOpenAIConfig(cfg, true); err == nil {
			t.Errorf("accepted invalid base %s", base)
		}
	}
	cfg = openAITestConfig(server.URL)
	cfg.BaseURL = server.URL + "/"
	got, err := normalizeOpenAIConfig(cfg, true)
	if err != nil || got.BaseURL != server.URL+"/v1" {
		t.Fatalf("normalized=%+v %v", got, err)
	}
}
func TestOpenAIRejectsCrossOriginRedirectAndRedactsErrors(t *testing.T) {
	var received atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Store(true) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		http.Error(w, "bad secret-key", 400)
	}))
	defer server.Close()
	cfg := openAITestConfig(server.URL)
	cfg.APIKey = "secret-key"
	if _, err := (&App{}).OpenAIListModels(cfg); err == nil || received.Load() {
		t.Fatalf("followed cross-origin redirect: %v", err)
	}
	_, err := openAIComplete(context.Background(), cfg, nil, true, nil)
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("unsafe error %v", err)
	}
}
func TestOpenAIAssemblesFragmentedToolCallsAndRejectsTruncation(t *testing.T) {
	var truncated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if truncated.Load() {
			openAITestStream(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "partial", "function": map[string]any{"name": "run_command", "arguments": `{"command":"touch`}}}}, "finish_reason": "length"}}})
			return
		}
		openAITestStream(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "abc", "function": map[string]any{"name": "write_", "arguments": `{"path":"hello`}}}}}}}, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"name": "file", "arguments": `.txt","content":"hi"}`}}}}, "finish_reason": "tool_calls"}}})
	}))
	defer server.Close()
	msg, err := openAIComplete(context.Background(), openAITestConfig(server.URL), nil, true, nil)
	if err != nil || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "write_file" || msg.ToolCalls[0].Function.Arguments != `{"path":"hello.txt","content":"hi"}` {
		t.Fatalf("bad assembly %+v %v", msg, err)
	}
	truncated.Store(true)
	if _, err := openAIComplete(context.Background(), openAITestConfig(server.URL), nil, true, nil); err == nil {
		t.Fatal("accepted truncated tool request")
	}
}

func TestOpenAIFullCodingTurnApprovalAndResume(t *testing.T) {
	a, configDir := newChatApp(t)
	busReset()
	t.Cleanup(busReset)
	cwd := t.TempDir()
	if _, err := a.db.Exec(`INSERT INTO workspaces(id,name,path,created_at) VALUES(1,'test',?,0)`, cwd); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string          `json:"model"`
			Messages []openAIMessage `json:"messages"`
			Tools    []any           `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body.Model != "override-model" || len(body.Tools) < 5 {
			t.Errorf("model/tools missing: %+v", body)
		}
		switch requests.Add(1) {
		case 1:
			openAITestStream(w, openAITestTool("write_file", `{"path":"hello.txt","content":"hello world"}`))
		case 2:
			last := body.Messages[len(body.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content.(string), "File written") {
				t.Errorf("missing tool result: %+v", last)
			}
			openAITestStream(w, openAITestText("Created hello.txt and checked the result."))
		default:
			found := false
			for _, m := range body.Messages {
				if m.Role == "tool" && m.ToolCallID == "call-1" {
					found = true
				}
			}
			if !found {
				t.Error("resume lost tool history")
			}
			openAITestStream(w, openAITestText("I remember the earlier edit."))
		}
	}))
	defer server.Close()
	cfg := openAITestConfig(server.URL)
	writeTestConfig(t, configDir, map[string]any{"providers": []any{map[string]any{"id": "local-agent", "providerId": "openai-compatible", "kind": "custom", "transport": "acp", "enabled": true, "openai": cfg}}})
	chat, err := a.CreateChat(Chat{WorkspaceID: 1, AgentKind: "local-agent", Transport: "acp", Model: "override-model", PermissionMode: "default"})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(chat.ID)
	// Start the real row-driven runtime before attaching the approval observer.
	if err := a.ensureRPC(chat, "acp"); err != nil {
		t.Fatal(err)
	}
	sess, _ := a.acpReg().get(id)
	approvals := 0
	sess.native.emitHook = func(kind string, v any) {
		if kind != "acp-req" {
			return
		}
		approvals++
		if _, err := os.Stat(filepath.Join(cwd, "hello.txt")); !os.IsNotExist(err) {
			t.Error("file changed before approval")
		}
		raw, _ := json.Marshal(v)
		var request struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(raw, &request)
		if err := a.AcpRespondPermission(id, request.ID, "allow"); err != nil {
			t.Error(err)
		}
	}
	if err := a.ChatSend(chat.ID, "Create hello.txt", nil); err != nil {
		t.Fatal(err)
	}
	openAITestWait(t, sess.native)
	b, err := os.ReadFile(filepath.Join(cwd, "hello.txt"))
	if err != nil || string(b) != "hello world" || approvals != 1 {
		t.Fatalf("edit=%q approvals=%d err=%v", b, approvals, err)
	}
	if err := a.ChatRestart(chat.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(chat.ID, "What did you change?", nil); err != nil {
		t.Fatal(err)
	}
	resumed, _ := a.acpReg().get(id)
	openAITestWait(t, resumed.native)
	if requests.Load() != 3 || len(resumed.native.messages) != 6 {
		t.Fatalf("requests=%d history=%+v", requests.Load(), resumed.native.messages)
	}
	if err := a.AcpStop(id); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatClear(chat.ID); err != nil {
		t.Fatal(err)
	}
	row, err := a.chatByID(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ClaudeSessionID != "" {
		t.Fatal("clear retained session ID")
	}
	var retained int
	if err := a.db.QueryRow(`SELECT count(*) FROM openai_sessions WHERE chat_id = ?`, id).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("clear retained model history: count=%d err=%v", retained, err)
	}
}

func TestOpenAIDenialPlanAndWorkspaceBoundary(t *testing.T) {
	for _, mode := range []string{"default", "plan", "auto"} {
		t.Run(mode, func(t *testing.T) {
			a := newTestApp(t)
			cwd := t.TempDir()
			cfg := openAITestConfig("http://localhost:1")
			if err := a.startOpenAI(AcpStartOpts{ID: "17", Cwd: cwd, OpenAI: &cfg}); err != nil {
				t.Fatal(err)
			}
			sess, _ := a.acpReg().get("17")
			s := sess.native
			s.mode = mode
			requests := 0
			s.emitHook = func(kind string, v any) {
				if kind == "acp-req" {
					requests++
					raw, _ := json.Marshal(v)
					var request struct {
						ID int64 `json:"id"`
					}
					_ = json.Unmarshal(raw, &request)
					if err := a.AcpRespondPermission("17", request.ID, "deny"); err != nil {
						t.Error(err)
					}
				}
			}
			output, failed := s.execute(context.Background(), openAIToolCall{ID: "w", Function: openAIFunction{Name: "write_file", Arguments: `{"path":"file.txt","content":"new"}`}})
			_, err := os.Stat(filepath.Join(cwd, "file.txt"))
			if mode == "auto" {
				if failed || err != nil {
					t.Fatalf("auto failed %s %v", output, err)
				}
			} else {
				if !failed || !os.IsNotExist(err) {
					t.Fatalf("denied edit ran: %s %v", output, err)
				}
			}
			if (mode == "default" && requests != 1) || (mode != "default" && requests != 0) {
				t.Fatalf("unexpected approvals=%d", requests)
			}
			if _, err := openAIWorkspacePath(cwd, "../escape"); err == nil {
				t.Error("path traversal allowed")
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(cwd, "outside")); err != nil {
				t.Fatal(err)
			}
			if _, err := openAIWorkspacePath(cwd, "outside/new.txt"); err == nil {
				t.Error("symlink escape allowed")
			}
			if err := a.AcpStop("17"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestOpenAIInterruptCancelsPendingApproval(t *testing.T) {
	a := newTestApp(t)
	cfg := openAITestConfig("http://localhost:1")
	cwd := t.TempDir()
	if err := a.startOpenAI(AcpStartOpts{ID: "18", Cwd: cwd, OpenAI: &cfg}); err != nil {
		t.Fatal(err)
	}
	sess, _ := a.acpReg().get("18")
	s := sess.native
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	s.emitHook = func(kind string, v any) {
		if kind == "acp-req" {
			close(ready)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.execute(ctx, openAIToolCall{ID: "w", Function: openAIFunction{Name: "write_file", Arguments: `{"path":"no.txt","content":"no"}`}})
	}()
	<-ready
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel left approval blocked")
	}
	if _, err := os.Stat(filepath.Join(cwd, "no.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled tool wrote a file")
	}
	if err := a.AcpRespondPermission("18", 101, "allow"); err == nil {
		t.Fatal("stale approval accepted")
	}
	_ = a.AcpStop("18")
}

func TestOpenAIFileToolsAndCommandCancellation(t *testing.T) {
	a := newTestApp(t)
	cwd := t.TempDir()
	cfg := openAITestConfig("http://localhost:1")
	if err := a.startOpenAI(AcpStartOpts{ID: "19", Cwd: cwd, OpenAI: &cfg}); err != nil {
		t.Fatal(err)
	}
	sess, _ := a.acpReg().get("19")
	s := sess.native
	s.mode = "auto"
	s.emitHook = func(string, any) {}
	run := func(name, args string) (string, bool) {
		t.Helper()
		return s.execute(context.Background(), openAIToolCall{ID: name, Function: openAIFunction{Name: name, Arguments: args}})
	}
	if out, failed := run("write_file", `{"path":"nested/main.txt","content":"alpha\nbeta\n"}`); failed {
		t.Fatal(out)
	}
	if out, failed := run("edit_file", `{"path":"nested/main.txt","old_text":"beta","new_text":"gamma"}`); failed {
		t.Fatal(out)
	}
	if out, failed := run("read_file", `{"path":"nested/main.txt","offset":2,"limit":1}`); failed || !strings.Contains(out, "2: gamma") {
		t.Fatalf("read %q failed=%v", out, failed)
	}
	if out, failed := run("search_files", `{"path":"nested","query":"gamma"}`); failed || !strings.Contains(out, "main.txt:2: gamma") {
		t.Fatalf("search %q failed=%v", out, failed)
	}
	if out, failed := run("run_command", `{"command":"printf command-ok"}`); failed || !strings.Contains(out, "command-ok") {
		t.Fatalf("command %q failed=%v", out, failed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, failed := s.execute(ctx, openAIToolCall{ID: "cancel", Function: openAIFunction{Name: "run_command", Arguments: `{"command":"touch cancelled"}`}}); !failed {
		t.Fatal("cancelled command succeeded")
	}
	if _, err := os.Stat(filepath.Join(cwd, "cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled command ran")
	}
	_ = a.AcpStop("19")
}

func TestOpenAICompactsToolHeavyTurnWithoutOrphanResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tools"]; ok {
			t.Error("summary request offered execution tools")
		}
		openAITestStream(w, openAITestText("Goal: fix the test. Earlier steps inspected files; continue from the latest tool result."))
	}))
	defer server.Close()
	a := newTestApp(t)
	cfg := openAITestConfig(server.URL)
	cfg.ContextWindow = 8192
	cfg.MaxOutputTokens = 1024
	if err := a.startOpenAI(AcpStartOpts{ID: "20", Cwd: t.TempDir(), OpenAI: &cfg}); err != nil {
		t.Fatal(err)
	}
	sess, _ := a.acpReg().get("20")
	s := sess.native
	s.emitHook = func(string, any) {}
	s.messages = []openAIMessage{{Role: "user", Content: "Fix the test"}}
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		s.messages = append(s.messages, openAIMessage{Role: "assistant", Content: "", ToolCalls: []openAIToolCall{{ID: id, Type: "function", Function: openAIFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}}}}, openAIMessage{Role: "tool", ToolCallID: id, Content: strings.Repeat("source code\n", 400)})
	}
	if err := s.compact(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.messages[0].Content.(string), "Earlier conversation summary") {
		t.Fatalf("no summary: %+v", s.messages[0])
	}
	for i, m := range s.messages {
		if m.Role == "tool" {
			if i == 0 || len(s.messages[i-1].ToolCalls) == 0 || s.messages[i-1].ToolCalls[0].ID != m.ToolCallID {
				t.Fatalf("orphan tool result: %+v", m)
			}
		}
	}
	_ = a.AcpStop("20")
}

func TestOpenAINativeEventsCarryToolInputsAndUsage(t *testing.T) {
	events := NormalizeAcpLine(`{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"edit-1","title":"edit_file: main.go","rawInput":{"path":"main.go","old_string":"old","new_string":"new"}}}}`)
	if len(events) != 1 || events[0].Input["new_string"] != "new" {
		t.Fatalf("tool input lost: %+v", events)
	}
	events = NormalizeAcpLine(`{"_burrow":"usage","inputTokens":2000,"outputTokens":100,"contextWindow":32768}`)
	if len(events) != 1 || events[0].Type != EvtContextUsage || events[0].ContextTokens != 2100 || events[0].ContextWindow != 32768 {
		t.Fatalf("usage lost: %+v", events)
	}
}
