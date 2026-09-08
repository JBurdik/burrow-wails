package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
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

	// maxInboundFrame caps a single inbound frame. write_pty pastes and
	// claude_send/acp_send prompts (plus their inline screenshot images)
	// are orders of magnitude smaller than this; the traffic that
	// legitimately approaches it is a write_text_file payload — that call
	// IS reachable over this socket (remoteAllowed maps it to WriteTextFile
	// under scopeOrchOperate, which remoteapi.go already documents as
	// unrestricted host file write, so a large legitimate write here is
	// expected, not exotic). Exceeding this limit closes the ENTIRE
	// connection rather than failing the one call — that is what
	// gorilla's SetReadLimit does, there is no per-call rejection — so
	// every other in-flight PTY and chat on the session goes down with it.
	// A per-call rejection would need a chunked/streaming inbound path,
	// which does not exist yet; that is the upgrade path for a later phase.
	// 64 MiB is a defensible bound in the meantime: generous enough that a
	// legitimate write_text_file of any file this app plausibly edits
	// clears it, while still not letting a hostile or buggy client force
	// an unbounded allocation.
	maxInboundFrame = 64 << 20

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

	// maxInFlightCalls caps how many calls ONE connection may have running at
	// once.
	//
	// Each call frame gets its own goroutine (see handle's read loop). It has
	// to: serve() used to run inline, so the next ReadMessage waited for
	// callApp to return, and the desktop uses exactly one connection for the
	// whole app. One slow command — generate_commit_message shells out to a
	// provider CLI on a 180 s budget (textgen.go) — meant no other frame was
	// read for that long: keystrokes to a terminal, create_pty, claude_send,
	// run_git all queued behind it, and the app looked frozen. Under the
	// Wails bindings this could not happen (one goroutine per call), so the
	// switch to this socket is what introduced it.
	//
	// The cap is what keeps "a goroutine per frame" from being an unbounded
	// spawn a client controls. 64 is far above anything the app itself does
	// concurrently (its widest fan-out is a handful of latest_npm_version
	// probes from the providers store) and far below a number that costs the
	// runtime anything. Exceeding it is answered with an error frame and
	// never by blocking the read loop — blocking it would reintroduce exactly
	// the head-of-line stall the goroutine exists to remove.
	maxInFlightCalls = 64
)

// ticketTTL is short because a ticket is only ever carried from LocalEndpoint()
// straight into a dial. It exists so a token never has to travel in a URL —
// browsers cannot set headers on a WS handshake, so the handshake credential
// is in the query string, and a single-use 30-second value is safe there in a
// way a long-lived token is not.
const ticketTTL = 30 * time.Second

type ticket struct {
	scopes []remoteScope
	// deviceID is the paired device this ticket was minted for, or "" for the
	// in-process desktop (LocalEndpoint). It is what makes a revoke able to
	// find the sockets it has to close.
	deviceID string
	expires  time.Time
}

type ticketStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]ticket
}

func newTicketStore() *ticketStore {
	return &ticketStore{ttl: ticketTTL, m: make(map[string]ticket)}
}

// issue mints a ticket for the in-process desktop. See issueFor for a paired
// device's.
func (s *ticketStore) issue(scopes []remoteScope) string {
	return s.issueFor("", scopes)
}

func (s *ticketStore) issueFor(deviceID string, scopes []remoteScope) string {
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
	s.m[tok] = ticket{scopes: scopes, deviceID: deviceID, expires: now.Add(s.ttl)}
	return tok
}

// dropDevice invalidates every outstanding (unredeemed) ticket minted for
// deviceID. Called by RevokeRemoteDevice alongside remoteWS.dropDevice — that
// closes already-open sockets, this stops a ticket issued moments before the
// revoke (and still inside its 30s window) from opening a new one afterwards.
// Without it, a revoke racing an in-flight /v2/ws-ticket → /v2/ws handshake
// leaves a full-authority connection that the row deletion can no longer
// reach, since handle() authorizes purely off the redeemed ticket and never
// re-reads the device row.
func (s *ticketStore) dropDevice(deviceID string) {
	if deviceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if v.deviceID == deviceID {
			delete(s.m, k)
		}
	}
}

// redeem consumes a ticket. Single use: a replayed handshake credential is
// worthless even if it leaks into a proxy log.
func (s *ticketStore) redeem(tok string) (ticket, bool) {
	if tok == "" {
		return ticket{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if subtle.ConstantTimeCompare([]byte(k), []byte(tok)) != 1 {
			continue
		}
		delete(s.m, k)
		if time.Now().After(v.expires) {
			return ticket{}, false
		}
		return v, true
	}
	return ticket{}, false
}

type remoteWS struct {
	app     *App
	tickets *ticketStore

	// Live connections by paired-device id, so RevokeRemoteDevice can close
	// them. A revoke that leaves yesterday's socket running is not a revoke,
	// and that socket is the whole app.
	//
	// Keyed by a per-connection id inside each device, so two connections
	// from one phone unregister independently. The desktop's own connections
	// (deviceID "") are never in here: there is no row to revoke, and being
	// reachable would let a revoke drop the window.
	connMu     sync.Mutex
	conns      map[string]map[uint64]func()
	nextConnID uint64

	// call is the seam the concurrency tests use to stand in a command that
	// blocks on demand; no real App method does so deterministically.
	// Production always runs callApp against the *App.
	call func(c remoteCmd, args map[string]json.RawMessage) (any, error)
}

func newRemoteWS(app *App, tickets *ticketStore) *remoteWS {
	h := &remoteWS{app: app, tickets: tickets, conns: map[string]map[uint64]func(){}}
	h.call = func(c remoteCmd, args map[string]json.RawMessage) (any, error) {
		return callApp(h.app, c, args)
	}
	return h
}

func (h *remoteWS) register(mux *http.ServeMux) {
	mux.HandleFunc("/v2/ws", h.handle)
}

// trackConn registers a connection's shutdown under its device, returning the
// function that removes it again. A deviceID of "" is the desktop and is not
// tracked (see the conns field).
func (h *remoteWS) trackConn(deviceID string, shutdown func()) func() {
	if deviceID == "" {
		return func() {}
	}
	h.connMu.Lock()
	h.nextConnID++
	id := h.nextConnID
	if h.conns[deviceID] == nil {
		h.conns[deviceID] = map[uint64]func(){}
	}
	h.conns[deviceID][id] = shutdown
	h.connMu.Unlock()

	return func() {
		h.connMu.Lock()
		defer h.connMu.Unlock()
		delete(h.conns[deviceID], id)
		if len(h.conns[deviceID]) == 0 {
			delete(h.conns, deviceID)
		}
	}
}

// dropDevice closes every connection a device holds. Called by
// RevokeRemoteDevice AFTER the row is gone, so a connection racing the revoke
// cannot re-authorize itself against a row that still exists.
func (h *remoteWS) dropDevice(deviceID string) {
	h.connMu.Lock()
	shutdowns := make([]func(), 0, len(h.conns[deviceID]))
	for _, fn := range h.conns[deviceID] {
		shutdowns = append(shutdowns, fn)
	}
	h.connMu.Unlock()

	// Outside the lock: each shutdown closes a socket, and the deferred
	// untrack on that connection's own goroutine takes this same lock.
	for _, fn := range shutdowns {
		fn()
	}
}

func (h *remoteWS) handle(w http.ResponseWriter, r *http.Request) {
	tk, ok := h.tickets.redeem(r.URL.Query().Get("ticket"))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	scopes := tk.scopes
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
	// Deferred immediately, before anything below can fail: every later
	// return from handle — a client disconnect, a read deadline expiring on
	// a vanished peer, a failed upgrade of the bus subscription, a panic in
	// this handler goroutine itself — has to close `done`, or the writer
	// goroutine sits blocked on its select for the lifetime of the process
	// and the connection leaks with it. Registering it here rather than at
	// each exit means no future early return can forget.
	//
	// (A panic inside a *call* no longer reaches these frames: each call
	// runs on its own goroutine, which recovers for itself — see the read
	// loop below.)
	shutdown := func() {
		closeOnce.Do(func() {
			close(done)
			conn.Close()
		})
	}
	defer shutdown()

	// Registered before the welcome frame, so a revoke landing during the
	// handshake still finds this connection rather than missing it by a
	// scheduling accident.
	defer h.trackConn(tk.deviceID, shutdown)()

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
	out <- welcomeFrame(envID, scopes, currentSeq())

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

	unsubscribe := busSubscribe(func(ev shellEvent) {
		// A numbered event travels as a shell frame so the client can record
		// where it got to; pty-data has no number and stays on the
		// unnumbered channel.
		fr := eventFrame(ev.Name, ev.Payload)
		if ev.Seq > 0 {
			fr = shellFrame([]shellEvent{ev})
		}
		select {
		case out <- fr:
		case <-done:
		default:
			// Queue full: drop the client rather than block the bus.
			//
			// Dropping is now recoverable for everything numbered — the
			// client reconnects, resumes from the seq it last saw, and the
			// ring hands back the gap (or a resync, if the gap outran 512
			// events). What is still lost is pty-data, which is not in the
			// ring on purpose: that is a hole in a terminal's scrollback
			// with no reattach to replay it, since pty-data-<id> is
			// emit-once and the daemon's own ring is only replayed on a
			// fresh attach. Phase dots, workspace and chat events survive.
			shutdown()
		}
	})
	defer unsubscribe()

	// One semaphore per connection, so a client that saturates its own cap
	// cannot throttle anybody else's.
	inFlight := make(chan struct{}, maxInFlightCalls)

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
		if f.T == "resume" {
			// Answered on the read loop rather than a goroutine: it is a
			// slice read under one mutex, and running it in order is what
			// keeps a resume's deltas ahead of the live events that follow.
			if evs, ok := resumeSince(f.Since); ok {
				if len(evs) > 0 {
					sendFrame(out, done, shellFrame(evs))
				}
			} else {
				sendFrame(out, done, resyncFrame())
			}
			continue
		}
		select {
		case inFlight <- struct{}{}:
		default:
			// Refuse rather than wait: the whole point of dispatching each
			// call on its own goroutine is that this loop keeps reading.
			// The client gets a real error for this id, so its promise
			// settles instead of hanging.
			sendFrame(out, done, errorFrame(f.ID, "too_many_calls", "connection has too many calls in flight"))
			continue
		}
		go func(f clientFrame) {
			// Defers are LIFO, so the slot is released after the recover
			// below has run — a panicking call must not leak its slot.
			defer func() { <-inFlight }()
			// A goroutine with no frame above it takes the whole process
			// down when it panics, and an exposed App method panicking is a
			// documented, expected event, not a theoretical one: several
			// dereference a.daemon with no nil guard (remoteapi.go), so a
			// list_pty_sessions on a startup race is enough. Before this
			// dispatch was a goroutine, serve() ran inline and net/http's
			// per-connection recover caught it — one socket dropped, the
			// client reconnected, the app lived. Before the /v2/ws flip,
			// Wails' own dispatcher caught it and returned a call error.
			// This restores that: one connection dies, the process does not.
			//
			// The error frame is best effort — shutdown() closes `done` and
			// the socket right behind it, so the writer may never flush it.
			// It is enqueued anyway because it costs nothing and, when it
			// does land, names the command that failed; the client settles
			// this call either way, since a close rejects everything still
			// pending on it.
			defer func() {
				e := recover()
				if e == nil {
					return
				}
				log.Printf("remote ws: %s panicked: %v\n%s", f.Cmd, e, debug.Stack())
				sendFrame(out, done, errorFrame(f.ID, "call_failed", fmt.Sprint(e)))
				shutdown()
			}()
			h.serve(f, granted, out, done)
		}(f)
	}
}

// sendFrame enqueues one frame for the connection's single writer. It waits
// for room (the queue drains as fast as the socket writes) but never past the
// connection's own shutdown, so it cannot outlive the writer it is feeding.
func sendFrame(out chan<- serverFrame, done <-chan struct{}, fr serverFrame) {
	select {
	case out <- fr:
	case <-done:
	}
}

// serve answers one call frame. It runs on its own goroutine, so calls on a
// connection are not serialized: a slow command delays only itself, and two
// calls submitted in order may execute — and reply — in either. That is what
// the client already assumes (transport.ts matches replies by id) and what
// the Wails bindings did before this socket existed, one goroutine per call.
func (h *remoteWS) serve(f clientFrame, granted map[remoteScope]bool, out chan<- serverFrame, done <-chan struct{}) {
	send := func(fr serverFrame) { sendFrame(out, done, fr) }

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

	result, err := h.call(cmd, f.Args)
	if err != nil {
		send(errorFrame(f.ID, "call_failed", err.Error()))
		return
	}
	send(replyFrame(f.ID, result))
}
