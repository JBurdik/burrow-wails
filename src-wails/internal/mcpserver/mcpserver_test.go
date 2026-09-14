package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer wires a Server directly at the httptest URL, bypassing New's
// port-based baseURL construction — the tests only care what callTool does to
// call.Arguments before it posts.
func newTestServer(url, chatID string) *Server {
	return &Server{baseURL: url + "/", token: "tok", cwd: "/tmp/repo", chatID: chatID, client: http.DefaultClient}
}

func callToolReq(name string, args map[string]any) rpcRequest {
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	return rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params}
}

// MCP is the primary door for the agents this feature exists for — Burrow
// injects burrow-mcp into every chat session — so a tool call has to carry
// BURROW_CHAT_ID the same way bin/burrow carries it for the CLI door, or
// spawn/wait_result/collect_results called as tools never see a parent.
func TestCallToolInjectsParentChatIDFromEnv(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := newTestServer(ts.URL, "42")
	resp := s.callTool(callToolReq("collect_results", map[string]any{}))
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	if id, ok := got["parent_chat_id"]; !ok || id != float64(42) {
		t.Fatalf("parent_chat_id = %v (ok=%v), want 42", id, ok)
	}
}

// Outside a chat session BURROW_CHAT_ID is unset, and a call must not invent a
// thread id out of nothing.
func TestCallToolOmitsParentChatIDWithoutEnv(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := newTestServer(ts.URL, "")
	s.callTool(callToolReq("collect_results", map[string]any{}))
	if _, ok := got["parent_chat_id"]; ok {
		t.Fatalf("parent_chat_id injected with no BURROW_CHAT_ID: %v", got)
	}
}

// A caller-supplied parent_chat_id (an agent explicitly targeting a different
// thread) is never overwritten by the environment's.
func TestCallToolDoesNotOverrideExplicitParentChatID(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := newTestServer(ts.URL, "42")
	s.callTool(callToolReq("collect_results", map[string]any{"parent_chat_id": float64(7)}))
	if id := got["parent_chat_id"]; id != float64(7) {
		t.Fatalf("parent_chat_id = %v, want the caller-supplied 7", id)
	}
}
