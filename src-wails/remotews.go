package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// outboundQueue is how many frames may wait for a slow client before its
	// connection is dropped. Dropping is deliberate: busEmit runs under
	// PhaseStore's emitMu, so a sink that blocks would stall every phase
	// change in the app, not just this client's view of it.
	outboundQueue = 256

	// maxInboundFrame bounds a single incoming message. The largest
	// legitimate frame this endpoint expects is a write_pty payload (a
	// paste) or a claude_send/acp_send call carrying a chat prompt plus a
	// handful of inline screenshot images — not a file upload, which has
	// its own path (save_temp_image, read/write_text_file, none of them
	// over this socket). 16 MiB covers that comfortably while still
	// bounding a hostile or buggy client's single frame to a fixed,
	// deliberate allocation instead of gorilla's unlimited default.
	maxInboundFrame = 16 << 20

	// pongWait/pingPeriod/writeWait implement the standard gorilla keepalive
	// pattern. The read deadline is refreshed only by a pong, so a client
	// that vanishes without a TCP FIN — a phone leaving the tailnet, a
	// sleeping laptop, a NAT rebind, the normal case for the client this
	// endpoint exists for — has its dead connection reclaimed within one
	// missed ping/pong cycle instead of leaving the reader (and, once a
	// write also blocks, the writer) parked in a syscall forever.
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
	writeWait  = 10 * time.Second
)

// ticketTTL is short because a ticket is only ever carried from LocalEndpoint()
// straight into a dial. It exists so a token never has to travel in a URL —
// browsers cannot set headers on a WS handshake, so the handshake credential
// is in the query string, and a single-use 30-second value is safe there in a
// way a long-lived token is not.
const ticketTTL = 30 * time.Second

type ticket struct {
	scopes  []remoteScope
	expires time.Time
}

type ticketStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]ticket
}

func newTicketStore() *ticketStore {
	return &ticketStore{ttl: ticketTTL, m: make(map[string]ticket)}
}

func (s *ticketStore) issue(scopes []remoteScope) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic sweep: the map only ever holds tickets issued in the last
	// 30 seconds, so it needs no background goroutine.
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.expires) {
			delete(s.m, k)
		}
	}
	s.m[tok] = ticket{scopes: scopes, expires: now.Add(s.ttl)}
	return tok
}

// redeem consumes a ticket. Single use: a replayed handshake credential is
// worthless even if it leaks into a proxy log.
func (s *ticketStore) redeem(tok string) ([]remoteScope, bool) {
	if tok == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if subtle.ConstantTimeCompare([]byte(k), []byte(tok)) != 1 {
			continue
		}
		delete(s.m, k)
		if time.Now().After(v.expires) {
			return nil, false
		}
		return v.scopes, true
	}
	return nil, false
}

type remoteWS struct {
	app     *App
	tickets *ticketStore
}

func newRemoteWS(app *App, tickets *ticketStore) *remoteWS {
	return &remoteWS{app: app, tickets: tickets}
}

func (h *remoteWS) register(mux *http.ServeMux) {
	mux.HandleFunc("/v2/ws", h.handle)
}

func (h *remoteWS) handle(w http.ResponseWriter, r *http.Request) {
	scopes, ok := h.tickets.redeem(r.URL.Query().Get("ticket"))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Bound a single incoming frame, and detect a peer that vanished without
	// closing the TCP connection (see the constants' comments above).
	conn.SetReadLimit(maxInboundFrame)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	granted := make(map[remoteScope]bool, len(scopes))
	for _, s := range scopes {
		granted[s] = true
	}

	// One outbound queue, one writer. Two goroutines must never write the
	// same websocket, and a single queue is also what makes ordering hold:
	// a reply and an event produced in that order arrive in that order.
	out := make(chan serverFrame, outboundQueue)
	done := make(chan struct{})
	var closeOnce sync.Once
	// shutdown closes done AND the underlying connection. Closing done alone
	// stops the writer goroutine, but the reader goroutine is blocked in a
	// plain conn.ReadMessage() with no select on done — it would otherwise
	// stay parked (and the goroutine + socket alive) until the client itself
	// sends something or disconnects. Conn.Close() is documented safe to
	// call concurrently with the reader/writer and is what actually unblocks
	// that read, so a full outbound queue really drops the connection
	// rather than just going quiet on writes.
	//
	// Deferred immediately: if an exposed App method panics inside callApp
	// (serve, below), the stack unwinds through serve and handle without
	// ever reaching the explicit shutdown() calls further down this
	// function, and net/http's per-request recover would otherwise leave
	// done unclosed forever — the writer goroutine would sit blocked on its
	// select for the lifetime of the process, one leaked goroutine per
	// panicking call. Several exposed methods are documented (remoteapi.go)
	// to dereference a.daemon with no nil guard, so this is not academic.
	shutdown := func() {
		closeOnce.Do(func() {
			close(done)
			conn.Close()
		})
	}
	defer shutdown()

	// The welcome frame is enqueued before the bus subscription exists and
	// before the writer goroutine starts, into a channel that is still
	// completely empty — so this send is structurally the first item ever
	// placed in `out` and cannot block. That also makes welcome
	// structurally the first frame the client ever receives: subscribing to
	// the bus AFTER this line means no bus event (e.g. pty-data-<id>, which
	// fires on every chunk of output from a live agent) can be enqueued
	// ahead of it.
	envID := ""
	if h.app != nil {
		envID = h.app.environmentID
	}
	out <- welcomeFrame(envID, scopes)

	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case f := <-out:
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteJSON(f); err != nil {
					shutdown()
					return
				}
			case <-ticker.C:
				// Ping comes from the writer's own select, so it never
				// races the frame writes above even though WriteControl is
				// separately documented safe to call concurrently with
				// them.
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
					shutdown()
					return
				}
			case <-done:
				return
			}
		}
	}()

	unsubscribe := busSubscribe(func(name string, payload any) {
		select {
		case out <- eventFrame(name, payload):
		case <-done:
		default:
			// Queue full: drop the client rather than block the bus.
			shutdown()
		}
	})
	defer unsubscribe()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			// shutdown() is deferred above and runs on this return, whether
			// this is a genuine client disconnect, a deadline expiring on a
			// vanished peer, or the outbound queue overflowing and closing
			// conn out from under this blocked read.
			return
		}
		f, err := decodeClientFrame(raw)
		if err != nil {
			// A malformed frame with no id cannot be replied to; log and
			// carry on rather than killing a working connection.
			log.Printf("remote ws: %v", err)
			continue
		}
		h.serve(f, granted, out, done)
	}
}

func (h *remoteWS) serve(f clientFrame, granted map[remoteScope]bool, out chan<- serverFrame, done <-chan struct{}) {
	send := func(fr serverFrame) {
		select {
		case out <- fr:
		case <-done:
		}
	}

	cmd, ok := remoteAllowed[f.Cmd]
	if !ok {
		send(errorFrame(f.ID, "unknown_command", "no such command "+f.Cmd))
		return
	}
	// Scope is checked per command, not once per connection: holding a ticket
	// is not authorization to call everything it could reach.
	if !granted[cmd.Scope] {
		send(errorFrame(f.ID, "forbidden", string(cmd.Scope)+" required"))
		return
	}

	result, err := callApp(h.app, cmd, f.Args)
	if err != nil {
		send(errorFrame(f.ID, "call_failed", err.Error()))
		return
	}
	send(replyFrame(f.ID, result))
}
