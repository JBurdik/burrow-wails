package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// HTTPServer is a minimal port of src-tauri's http_server/ (axum + WS) —
// lets a browser reach the same App methods and event stream as the native
// window. Auth/Tailscale integration from the Rust version isn't ported yet
// (see plan phase 6/7); this covers the WS event fan-out + a small JSON-RPC
// surface for PTY control, enough for `burrow-web`-style remote use.
type HTTPServer struct {
	app *App

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewHTTPServer(app *App) *HTTPServer {
	return &HTTPServer{app: app, clients: make(map[*websocket.Conn]struct{})}
}

func (s *HTTPServer) Broadcast(eventName string, payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := map[string]any{"event": eventName, "data": payload}
	for c := range s.clients {
		if err := c.WriteJSON(msg); err != nil {
			c.Close()
			delete(s.clients, c)
		}
	}
}

func (s *HTTPServer) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/rpc/", s.handleRPC)
	log.Printf("http server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *HTTPServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.clients[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

type rpcRequest struct {
	Shell string   `json:"shell"`
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd"`
	ID    string   `json:"id"`
	Data  string   `json:"data"`
}

// handleRPC exposes a handful of App methods as POST /rpc/<method>.
// Deliberately small: /rpc/create-pty, /rpc/write-pty. Extend as the
// browser client needs more (see dispatch.rs for the full Rust surface).
func (s *HTTPServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	method := r.URL.Path[len("/rpc/"):]
	switch method {
	case "create-pty":
		id, err := s.app.CreatePty(req.Shell, req.Args, req.Cwd, nil)
		writeJSON(w, map[string]any{"id": id}, err)
	case "write-pty":
		err := s.app.WritePty(req.ID, req.Data)
		writeJSON(w, map[string]any{"ok": err == nil}, err)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v map[string]any, err error) {
	if err != nil {
		v["error"] = err.Error()
		w.WriteHeader(http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
