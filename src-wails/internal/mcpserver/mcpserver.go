// Package mcpserver implements a minimal Model Context Protocol server
// (JSON-RPC 2.0 over stdio) exposing a handful of Burrow tools — a
// hand-rolled implementation rather than a vendored SDK, matching just
// enough of the MCP spec (initialize, tools/list, tools/call) for an
// agent client to drive it. Reimplements the intent of
// src-tauri/src/burrow_mcp_stdio.rs / burrow_mcp_core.rs, not a 1:1 port.
package mcpserver

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// Server owns a direct SQLite connection to workspaces.db — it runs as a
// separate process from the main app, so it can't share the app's *sql.DB;
// SQLite's own locking handles the concurrent access.
type Server struct {
	db      *sql.DB
	sessDir string
	tools   map[string]func(params json.RawMessage) (any, error)
}

func New(db *sql.DB, sessionDir string) *Server {
	s := &Server{db: db, sessDir: sessionDir}
	s.tools = map[string]func(json.RawMessage) (any, error){
		"spawn_agent": s.toolSpawnAgent,
	}
	return s
}

// Serve reads JSON-RPC requests line-delimited from r and writes responses
// to w, until r is exhausted (stdin closed).
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	dec := bufio.NewScanner(r)
	dec.Buffer(make([]byte, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(w)

	for dec.Scan() {
		line := dec.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		resp := s.handle(req)
		if resp != nil {
			_ = enc.Encode(resp)
		}
	}
	return dec.Err()
}

func (s *Server) handle(req rpcRequest) *rpcResponse {
	switch req.Method {
	case "initialize":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]string{"name": "burrow", "version": appVersionPlaceholder},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}}
	case "notifications/initialized":
		return nil // no response for notifications
	case "tools/list":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": s.listTools()}}
	case "tools/call":
		return s.handleToolCall(req)
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
	}
}

// appVersionPlaceholder avoids importing the main package (would create an
// import cycle since main imports this package); callers needing the real
// version can extend Server with a Version field later.
const appVersionPlaceholder = "dev"

func (s *Server) listTools() []Tool {
	return []Tool{
		{Name: "spawn_agent", Description: "Spawn a sub-agent in a new terminal tab", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{
				"cwd": map[string]any{"type": "string"},
				"cmd": map[string]any{"type": "string"},
			}, "required": []string{"cwd", "cmd"},
		}},
	}
}

func (s *Server) handleToolCall(req rpcRequest) *rpcResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	fn, ok := s.tools[call.Name]
	if !ok {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "unknown tool: " + call.Name}}
	}
	result, err := fn(call.Arguments)
	if err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"isError": true,
			"content": []map[string]string{{"type": "text", "text": err.Error()}},
		}}
	}
	text, _ := json.Marshal(result)
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(text)}},
	}}
}

// toolSpawnAgent drops a request dir under sessDir/requests, the same
// file-based transport the `burrow` CLI's `spawn` subcommand uses — the
// app's frontend poll loop (TakeSpawnRequests) picks it up and opens the
// tab. Kept here rather than duplicated logic so both paths agree.
func (s *Server) toolSpawnAgent(params json.RawMessage) (any, error) {
	var p struct {
		Cwd string `json:"cwd"`
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if s.sessDir == "" {
		return nil, fmt.Errorf("session dir not configured")
	}
	reqDir := fmt.Sprintf("%s/requests/req.%d", s.sessDir, time.Now().UnixNano())
	if err := os.MkdirAll(reqDir, 0o755); err != nil {
		return nil, err
	}
	writeField := func(name, val string) error { return os.WriteFile(reqDir+"/"+name, []byte(val), 0o644) }
	if err := writeField("cwd", p.Cwd); err != nil {
		return nil, err
	}
	if err := writeField("cmd", p.Cmd); err != nil {
		return nil, err
	}
	if err := writeField("ws", p.Cwd); err != nil {
		return nil, err
	}
	if err := writeField("ready", ""); err != nil {
		return nil, err
	}
	return map[string]string{"status": "queued"}, nil
}
