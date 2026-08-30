package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// The mobile web client (src/mobile/, built by `pnpm build:mobile`) is
// baked into the binary so the .app bundle can serve it with no extra
// packaging step. The checked-in dir holds only .gitkeep — CI/`just
// build` runs the vite build before `wails build`.
//
//go:embed all:dist-mobile
var mobileAssets embed.FS

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
	srv     *http.Server
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
	msg := map[string]any{"event": eventName, "payload": payload}
	for c := range s.clients {
		if err := c.WriteJSON(msg); err != nil {
			c.Close()
			delete(s.clients, c)
		}
	}
}

// Close stops the listener and drops every WS client. Safe to call twice.
func (s *HTTPServer) Close() error {
	s.mu.Lock()
	for c := range s.clients {
		c.Close()
		delete(s.clients, c)
	}
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

func (s *HTTPServer) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/rpc/", s.handleRPC)
	// Both unauthenticated on purpose: a plain browser GET cannot send an
	// Authorization header, so the shell and the health probe have to be
	// open. Everything that touches a PTY sits behind the token.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/", s.handleAssets)
	srv := &http.Server{Addr: addr, Handler: mux}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	log.Printf("http server listening on %s", addr)
	return srv.ListenAndServe()
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
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var c wsCall
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		// {subscribe: "<event>"} needs no server-side bookkeeping: Broadcast
		// fans every event out to every client and api.ts routes by its own
		// handler map. Accept and ignore.
		if c.Command == "" {
			continue
		}
		result, err := s.dispatch(c)
		reply := map[string]any{"id": c.ID}
		if err != nil {
			reply["error"] = err.Error()
		} else {
			reply["result"] = result
		}
		s.mu.Lock()
		werr := conn.WriteJSON(reply)
		s.mu.Unlock()
		if werr != nil {
			return
		}
	}
}

// wsCall is the client->server frame from src/mobile/api.ts: either a
// call ({id, command, args}) or a subscribe ({subscribe}).
type wsCall struct {
	ID        int    `json:"id"`
	Command   string `json:"command"`
	Subscribe string `json:"subscribe"`
	Args      wsArgs `json:"args"`
}

// wsArgs is the union of every argument object the mobile client sends.
// Keys are camelCase because that is what api.ts puts on the wire.
type wsArgs struct {
	ID          string `json:"id"`
	WorkspaceID int64  `json:"workspaceId"`
	Data        []int  `json:"data"`
	Cols        uint16 `json:"cols"`
	Rows        uint16 `json:"rows"`
}

// dispatch maps a WS call onto an App method. Deliberately an explicit
// allow-list, not reflection: this surface is reachable from the tailnet,
// so every remotely-invokable method is one someone typed here on purpose.
//
// ponytail: terminal-only. remote_list_chats/remote_create_chat/claude_send/
// acp_send are what ChatsView wants, but RemoteListChats/RemoteCreateChat are
// still stubs in stubs.go — wire them here once they do something.
func (s *HTTPServer) dispatch(c wsCall) (any, error) {
	switch c.Command {
	case "list_workspaces":
		return s.app.ListWorkspaces()
	case "list_terminal_tabs":
		return s.app.ListTerminalTabs(c.Args.WorkspaceID)
	case "write_pty":
		return nil, s.app.WritePty(c.Args.ID, c.Args.Data)
	case "resize_pty":
		return nil, s.app.ResizePty(c.Args.ID, c.Args.Cols, c.Args.Rows)
	default:
		return nil, fmt.Errorf("unknown command %q", c.Command)
	}
}

// handleAssets serves the embedded mobile bundle. `/` maps to mobile.html
// (vite names the entry after its input file, and renaming it in the build
// buys nothing).
func (s *HTTPServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(mobileAssets, "dist-mobile/app")
	if err != nil {
		http.Error(w, "assets unavailable", http.StatusInternalServerError)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "mobile.html"
	}
	if _, err := fs.Stat(sub, name); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, sub, name)
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
