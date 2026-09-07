package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// The tailnet listener. What used to be here — a hand-written `dispatch` over
// a dozen commands, a shared `http.token` that travelled in a query
// parameter, a Broadcast fan-out with its own client set, and a /pair that
// handed that one token to every device — is gone as of phase 6, along with
// the client that needed it (src/mobile/api.ts).
//
// What is left is a listener with three jobs:
//
//	/v2/ws, /v2/pair, /v2/ws-ticket   the real surface (remotews.go,
//	                                   remoteauth.go), mounted from the app so
//	                                   the ticket store and the live-connection
//	                                   registry are the SAME ones the desktop
//	                                   and RevokeRemoteDevice use
//	/healthz                          unauthenticated liveness probe
//	/                                 the embedded PWA bundle
//
// It binds loopback only and refuses to start behind `tailscale funnel`; both
// checks live in remoteguard.go and both fail closed. The way in from the
// tailnet is `tailscale serve`, which terminates real HTTPS and forwards here.
type HTTPServer struct {
	app *App

	mu  sync.Mutex
	srv *http.Server
}

// upgrader is shared with remotews.go's /v2/ws handler.
//
// CheckOrigin is permissive because there is no cookie or ambient credential
// to steal with a cross-origin handshake: /v2/ws authenticates with a
// single-use ticket the page has to have been given, so an attacker's page
// opening a socket gets a 401.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewHTTPServer(app *App) *HTTPServer {
	return &HTTPServer{app: app}
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

// The mobile web client (src/mobile/, built by `pnpm build:mobile`) is
// baked into the binary so the .app bundle can serve it with no extra
// packaging step. The checked-in dir holds only .gitkeep — CI/`just
// build` runs the vite build before `wails build`.
//
//go:embed all:dist-mobile
var mobileAssets embed.FS

// Close stops the listener. Safe to call twice.
func (s *HTTPServer) Close() error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

func (s *HTTPServer) ListenAndServe(addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.mux()}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	log.Printf("http server listening on %s", addr)
	return srv.ListenAndServe()
}

// mux is the whole route table, built separately from the listener so a test
// can assert the set of routes rather than a copy of it — the v1 paths being
// GONE is a property of this function, and a test that rebuilt the table
// would only be testing its own rebuild.
func (s *HTTPServer) mux() *http.ServeMux {
	mux := http.NewServeMux()

	// The app's OWN handlers, not new ones. A second ticket store would mean
	// a ticket minted by /v2/ws-ticket is unknown to the handler that redeems
	// it, and a second remoteWS would keep its live connections in a registry
	// RevokeRemoteDevice never looks at.
	if s.app != nil {
		if s.app.remoteWS != nil {
			s.app.remoteWS.register(mux)
		}
		if s.app.remoteAuth != nil {
			s.app.remoteAuth.register(mux)
		}
	}

	// Unauthenticated on purpose: a plain browser GET cannot send an
	// Authorization header, so the PWA shell and the health probe have to be
	// open. They carry no data — the shell is a static bundle that then has to
	// pair like any other client.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/", s.handleAssets)
	return mux
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
