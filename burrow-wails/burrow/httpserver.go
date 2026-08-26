package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gorilla/websocket"
)

// HTTPServer is a minimal port of src-tauri's http_server/ (axum + WS) —
// lets a browser reach the same App methods and event stream as the native
// window. Covers the WS event fan-out, a small JSON-RPC surface for PTY
// control, and bearer-token auth (a random token generated per app-data
// dir, matching the shape of the Rust version's auth.rs without porting
// its exact session-cookie mechanics). Tailscale integration is a thin
// `tailscale serve` shell-out (see TailscaleServe below), not a full port
// of tailscale.rs's embedded-tsnet approach.
type HTTPServer struct {
	app   *App
	token string

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewHTTPServer(app *App) *HTTPServer {
	return &HTTPServer{app: app, clients: make(map[*websocket.Conn]struct{}), token: loadOrCreateHTTPToken()}
}

// loadOrCreateHTTPToken persists a random bearer token in the app data dir
// so remote clients (and this app's own future sessions) can authenticate
// consistently across restarts.
func loadOrCreateHTTPToken() string {
	dataDir, err := appDataDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dataDir, "http.token")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b)
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	_ = os.WriteFile(path, []byte(token), 0o600)
	return token
}

func (s *HTTPServer) authorized(r *http.Request) bool {
	if s.token == "" {
		return false // token generation/persistence failed — fail closed
	}
	got := r.Header.Get("Authorization")
	want := "Bearer " + s.token
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
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
	// Browsers can't set custom headers on the WS handshake, so the token
	// travels as a query param here instead of Authorization.
	if s.token != "" && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
	Cwd  string `json:"cwd"`
	ID   string `json:"id"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
	Data []int  `json:"data"`
}

// handleRPC exposes a handful of App methods as POST /rpc/<method>.
// Deliberately small: /rpc/create-pty, /rpc/write-pty. Extend as the
// browser client needs more (see dispatch.rs for the full Rust surface).
func (s *HTTPServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
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
		err := s.app.CreatePty(req.ID, req.Cwd, req.Cols, req.Rows)
		writeJSON(w, map[string]any{"id": req.ID}, err)
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
