// Package mcpserver exposes Burrow's control verbs to an agent client as MCP
// tools (JSON-RPC 2.0 over stdio).
//
// It is a thin translator, not a second implementation: `tools/list` is the
// app's own verb registry fetched over the loopback control API, and
// `tools/call` is one POST to that API. So an MCP tool cannot drift from what
// `burrow <verb>` does — same verb, same code, different door. Hand-rolled
// rather than a vendored SDK: initialize + tools/list + tools/call is the whole
// surface an agent client needs.
package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// verb is one entry of the app's registry, as served by /v1/_verbs.
type verb struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Args    []struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Desc     string `json:"desc"`
		Required bool   `json:"required"`
	} `json:"args"`
}

// Server talks to a running Burrow over loopback. Version is reported in the
// initialize handshake so a client's logs show which app it's driving.
type Server struct {
	baseURL string
	token   string
	cwd     string
	version string
	client  *http.Client
}

// New returns a server for the app listening on port, authenticating with
// token. cwd is the directory verbs resolve "this repo" from — the MCP process
// inherits it from the agent session that spawned it.
func New(port int, token, cwd, version string) *Server {
	return &Server{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d/v1/", port),
		token:   token,
		cwd:     cwd,
		version: version,
		// No timeout: wait_result blocks for as long as a sub-agent takes, and
		// the verb enforces its own deadline.
		client: &http.Client{},
	}
}

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
		if resp := s.handle(req); resp != nil {
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
			"serverInfo":      map[string]string{"name": "burrow", "version": s.version},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}}
	case "notifications/initialized":
		return nil // notifications get no response
	case "tools/list":
		tools, err := s.listTools()
		if err != nil {
			return errorResponse(req.ID, -32603, err.Error())
		}
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}}
	case "tools/call":
		return s.callTool(req)
	default:
		return errorResponse(req.ID, -32601, "method not found: "+req.Method)
	}
}

func errorResponse(id json.RawMessage, code int, msg string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// listTools turns the app's registry into MCP tool definitions. Types come
// straight from the registry, so a client sends 42 rather than "42" where the
// verb wants a number.
func (s *Server) listTools() ([]Tool, error) {
	var verbs []verb
	if err := s.post("_verbs", nil, &verbs); err != nil {
		return nil, fmt.Errorf("Burrow is not reachable: %w", err)
	}
	tools := make([]Tool, 0, len(verbs))
	for _, v := range verbs {
		props := map[string]any{}
		required := []string{}
		for _, a := range v.Args {
			t := a.Type
			if t == "" {
				t = "string"
			}
			props[a.Name] = map[string]any{"type": t, "description": a.Desc}
			if a.Required {
				required = append(required, a.Name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		tools = append(tools, Tool{Name: v.Name, Description: v.Summary, InputSchema: schema})
	}
	return tools, nil
}

func (s *Server) callTool(req rpcRequest) *rpcResponse {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return errorResponse(req.ID, -32602, err.Error())
	}
	if call.Arguments == nil {
		call.Arguments = map[string]any{}
	}
	// Every verb resolves "this repo" from cwd; the model never has to pass it.
	if _, ok := call.Arguments["cwd"]; !ok && s.cwd != "" {
		call.Arguments["cwd"] = s.cwd
	}

	var result json.RawMessage
	if err := s.post(call.Name, call.Arguments, &result); err != nil {
		// A failed tool call is a result with isError, not a protocol error: the
		// model should read the reason and adapt, not see a broken transport.
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"isError": true,
			"content": []map[string]string{{"type": "text", "text": err.Error()}},
		}}
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(result)}},
	}}
}

func (s *Server) post(verb string, body any, out any) error {
	payload := []byte("{}")
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(http.MethodPost, s.baseURL+verb, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		// The API answers errors as {"error": "..."}; surface that text, since it
		// is written for whoever called the verb.
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
