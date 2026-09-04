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
	"math/big"
	"net/http"
	"os"
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

	pairMu       sync.Mutex
	pairCode     string
	pairFailures int
}

// pairMaxFailures locks pairing after this many wrong codes. Six digits
// against five guesses is 5-in-a-million, and /pair is only reachable from
// the tailnet — but the endpoint has to be unauthenticated (that is the
// point of pairing), so the attempt budget is what keeps it honest.
const pairMaxFailures = 5

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewHTTPServer(app *App) *HTTPServer {
	return &HTTPServer{
		app:      app,
		clients:  make(map[*websocket.Conn]struct{}),
		token:    loadOrCreateHTTPToken(),
		pairCode: randomPairCode(),
	}
}

// randomPairCode returns six uniformly random digits. Deliberately NOT
// derived from the bearer token: an earlier build showed the token's first
// six characters as a "pairing code", which leaked token material into the
// UI and never actually authenticated anything.
func randomPairCode() string {
	digits := make([]byte, 6)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return ""
		}
		digits[i] = byte('0' + n.Int64())
	}
	return string(digits)
}

// PairCode is the code to show in Settings. Empty means pairing is locked
// out and needs a regenerate.
func (s *HTTPServer) PairCode() string {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	if s.pairFailures >= pairMaxFailures {
		return ""
	}
	return s.pairCode
}

// RegeneratePairCode issues a fresh code and clears any lockout.
func (s *HTTPServer) RegeneratePairCode() string {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	s.pairCode = randomPairCode()
	s.pairFailures = 0
	return s.pairCode
}

// handlePair trades a correct pairing code for the bearer token, so a phone
// never has to be told the 48-character token. Unauthenticated by
// necessity; single-use (a success rotates the code) and budgeted (see
// pairMaxFailures).
func (s *HTTPServer) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.pairMu.Lock()
	defer s.pairMu.Unlock()

	if s.pairFailures >= pairMaxFailures {
		http.Error(w, "pairing locked — regenerate the code in Settings", http.StatusTooManyRequests)
		return
	}
	if s.token == "" || s.pairCode == "" ||
		subtle.ConstantTimeCompare([]byte(req.Code), []byte(s.pairCode)) != 1 {
		s.pairFailures++
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}

	// Single use: burn the code so a shoulder-surfed screen stays useless.
	s.pairCode = randomPairCode()
	s.pairFailures = 0
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": s.token})
}

// loadOrCreateHTTPToken persists the tailnet bearer token in the app data dir
// so remote clients (and this app's own future sessions) authenticate
// consistently across restarts. The loopback control API has its own token
// (control.token) — one compromised surface should not hand over the other.
func loadOrCreateHTTPToken() string {
	dataDir, err := appDataDir()
	if err != nil {
		return ""
	}
	return loadOrCreateToken(dataDir, "http.token")
}

// randomHex returns n random bytes hex-encoded, or "" if the system RNG fails
// (callers fail closed rather than fall back to a guessable token).
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
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
	mux.HandleFunc("/pair", s.handlePair)
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
	ID          string         `json:"id"`
	WorkspaceID int64          `json:"workspaceId"`
	Data        []int          `json:"data"`
	Cols        uint16         `json:"cols"`
	Rows        uint16         `json:"rows"`
	Text        string         `json:"text"`
	SessionId   string         `json:"sessionId"`
	RequestId   string         `json:"requestId"`
	Response    map[string]any `json:"response"`
	RpcId       int64          `json:"rpcId"`
	OptionId    string         `json:"optionId"`
	AgentKind   string         `json:"agentKind"`
}

// dispatch maps a WS call onto an App method. Deliberately an explicit
// allow-list, not reflection: this surface is reachable from the tailnet,
// so every remotely-invokable method is one someone typed here on purpose.
//
// The write commands below (claude_send, acp_send, claude_respond_control,
// acp_respond_permission, remote_create_chat) are thin wrappers over the same
// App methods the desktop UI already calls — no new agent-control logic
// here, just exposing it on the tailnet transport.
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
	case "list_pty_sessions":
		return s.app.ListPtySessions()
	case "remote_list_chats":
		return s.app.RemoteListChats()
	case "remote_create_chat":
		return s.app.RemoteCreateChat(c.Args.WorkspaceID, c.Args.AgentKind)
	case "claude_send":
		return nil, s.app.ClaudeSend(c.Args.ID, c.Args.Text, c.Args.SessionId, nil)
	case "acp_send":
		_, err := s.app.AcpSend(c.Args.ID, c.Args.Text, nil)
		return nil, err
	case "claude_respond_control":
		return nil, s.app.ClaudeRespondControl(c.Args.ID, c.Args.RequestId, c.Args.Response)
	case "acp_respond_permission":
		return nil, s.app.AcpRespondPermission(c.Args.ID, c.Args.RpcId, c.Args.OptionId)
	default:
		return nil, fmt.Errorf("unknown command %q", c.Command)
	}
}

// handleAssets serves the embedded mobile bundle. `/` maps to mobile.html
// (vite names the entry after its input file, and renaming it in the build
// buys nothing).
func (s *HTTPServer) handleAssets(w http.ResponseWriter, r *http.Request) {
	// //go:embed snapshots the bundle at compile time, and `wails dev` only
	// recompiles when Go source actually changes — so during mobile UI work a
	// fresh `pnpm build:mobile` would otherwise need a dev-server restart to
	// show up. Point BURROW_DEV_MOBILE at dist-mobile/app to serve from disk.
	var sub fs.FS
	if dir := os.Getenv("BURROW_DEV_MOBILE"); dir != "" {
		sub = os.DirFS(dir)
	} else {
		var err error
		if sub, err = fs.Sub(mobileAssets, "dist-mobile/app"); err != nil {
			http.Error(w, "assets unavailable", http.StatusInternalServerError)
			return
		}
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
