package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
	"time"
)

// outboundQueue is how many frames may wait for a slow client before its
// connection is dropped. Dropping is deliberate: busEmit runs under
// PhaseStore's emitMu, so a sink that blocks would stall every phase change
// in the app, not just this client's view of it.
const outboundQueue = 256

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
	// sends something or disconnects. Conn.Close() is documented safe to call
	// concurrently with the reader/writer and is what actually unblocks that
	// read, so a full outbound queue really drops the connection rather than
	// just going quiet on writes.
	shutdown := func() {
		closeOnce.Do(func() {
			close(done)
			conn.Close()
		})
	}

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

	go func() {
		for {
			select {
			case f := <-out:
				if err := conn.WriteJSON(f); err != nil {
					shutdown()
					return
				}
			case <-done:
				return
			}
		}
	}()

	envID := ""
	if h.app != nil {
		envID = h.app.environmentID
	}
	out <- welcomeFrame(envID, scopes)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			shutdown()
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
