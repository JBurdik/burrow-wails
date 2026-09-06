package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTicketIsSingleUse(t *testing.T) {
	s := newTicketStore()
	tok := s.issue([]remoteScope{scopeOrchRead})
	if _, ok := s.redeem(tok); !ok {
		t.Fatal("fresh ticket did not redeem")
	}
	if _, ok := s.redeem(tok); ok {
		t.Fatal("ticket redeemed twice")
	}
}

func TestTicketExpires(t *testing.T) {
	s := newTicketStore()
	s.ttl = 10 * time.Millisecond
	tok := s.issue([]remoteScope{scopeOrchRead})
	time.Sleep(30 * time.Millisecond)
	if _, ok := s.redeem(tok); ok {
		t.Fatal("expired ticket redeemed")
	}
}

func TestTicketRejectsUnknown(t *testing.T) {
	s := newTicketStore()
	if _, ok := s.redeem("not-a-ticket"); ok {
		t.Fatal("unknown ticket redeemed")
	}
}

// dialTestWS starts the handler on a test server and returns a connection
// that has already consumed the welcome frame.
func dialTestWS(t *testing.T, app *App) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	tickets := newTicketStore()
	h := newRemoteWS(app, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok := tickets.issue([]remoteScope{scopeOrchRead, scopeOrchOperate, scopeTerminal})
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("welcome: %v", err)
	}
	if welcome.T != "welcome" {
		t.Fatalf("first frame was %q, want welcome", welcome.T)
	}
	return conn, srv
}

func TestWSRejectsMissingTicket(t *testing.T) {
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws"
	if _, resp, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		t.Fatal("connected with no ticket")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v (%v)", resp, err)
	}
}

func TestWSCallGetsAReply(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{environmentID: "env-test"})

	// environment_id is safe on a bare &App{}: it reads a field. Do not use
	// get_pty_foreground or list_pty_sessions here — both dereference
	// a.daemon with no nil guard and would panic instead of erroring.
	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.T != "reply" || f.ID != 1 {
		t.Fatalf("bad reply: %+v", f)
	}
	if f.Result != "env-test" {
		t.Fatalf("reply carried %v, want the environment id", f.Result)
	}
}

func TestWSUnknownCommandIsAnErrorReplyNotADrop(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{})

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 2, Cmd: "telepathy"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.Error == nil || f.ID != 2 {
		t.Fatalf("unknown command must reply with an error carrying the id: %+v", f)
	}
}

func TestWSForwardsBusEvents(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{})

	// The welcome frame is now enqueued and read before this connection
	// subscribes to the bus (so it is guaranteed to arrive first, ahead of
	// any live event), which means there is no longer a signal available to
	// this test that the subscription has landed by the time dialTestWS
	// returns. Rather than bet on a fixed sleep, retry the emit against a
	// short per-attempt read deadline until the event frame shows up or an
	// overall deadline is exceeded — this proves delivery instead of timing.
	var f serverFrame
	overall := time.Now().Add(2 * time.Second)
	for {
		busEmit("phase-pty:7", map[string]string{"state": "running"})
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		err := conn.ReadJSON(&f)
		if err == nil {
			break
		}
		if time.Now().After(overall) {
			t.Fatalf("no event frame arrived: %v", err)
		}
	}
	if f.T != "event" || f.Name != "phase-pty:7" {
		t.Fatalf("bad event frame: %+v", f)
	}
}

// TestWSDropsClientWhenOutboundQueueFills exercises the design's third named
// property directly: a full outbound queue drops the connection rather than
// blocking busEmit. The client here never reads, so nothing ever drains the
// kernel socket buffers on either end of the connection.
//
// A burst of small events is not reliable bait for this: on a local
// connection the writer goroutine can drain a few hundred tiny JSON frames
// faster than a single busy producer goroutine can enqueue them, so the
// per-connection queue (outboundQueue = 256) never actually fills — this was
// tried and was flaky (it raced the writer instead of proving the property).
// Instead each emitted event payload is large enough (well past this
// machine's default 128 KiB TCP send/receive buffers, sysctl
// net.inet.tcp.sendspace/recvspace) that the writer's very first WriteJSON
// blocks solidly inside the network write syscall — since nothing is
// draining those kernel buffers, that block does not clear. Once the writer
// is genuinely wedged, busEmit's non-blocking sends (which never touch the
// network themselves) fill the remaining queue slots and the sink's
// queue-full branch fires deterministically, not probabilistically.
func TestWSDropsClientWhenOutboundQueueFills(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	conn, _ := dialTestWS(t, &App{})

	bigPayload := map[string]string{"state": strings.Repeat("x", 512*1024)}
	for i := 0; i < outboundQueue+16; i++ {
		busEmit("phase-pty:7", bigPayload)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("server did not drop the connection when its outbound queue filled")
	}
	// A plain read timeout is ALSO a non-nil error, and would be
	// indistinguishable from a genuine close on `err == nil` alone: if the
	// sink's queue-full branch silently dropped the event instead of
	// calling shutdown(), the connection would stay open, nothing would
	// arrive inside the deadline, and this test would pass for the wrong
	// reason. Require the error to actually be the read deadline expiring
	// on a still-open connection to be ruled OUT, not merely present.
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("read timed out rather than observing a dropped connection: %v", err)
	}
}

func TestWSScopeIsEnforcedPerCommand(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Read-only ticket: a terminal:operate command must be refused even
	// though the connection is authenticated.
	tok := tickets.issue([]remoteScope{scopeOrchRead})
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "kill_pty"}); err != nil {
		t.Fatal(err)
	}
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	if f.Error == nil || f.Error.Code != "forbidden" {
		t.Fatalf("read-only ticket was allowed to kill a pty: %+v", f)
	}
}

func TestLocalEndpointIssuesAUsableTicket(t *testing.T) {
	app := &App{environmentID: "env-test"}
	app.tickets = newTicketStore()
	app.hookPort = 1234

	info := app.LocalEndpoint()
	if info.EnvironmentID != "env-test" {
		t.Errorf("environment id not carried: %+v", info)
	}
	if !strings.Contains(info.WSURL, "127.0.0.1:1234/v2/ws") {
		t.Errorf("bad ws url: %q", info.WSURL)
	}
	if info.Ticket == "" {
		t.Fatal("no ticket issued")
	}
	scopes, ok := app.tickets.redeem(info.Ticket)
	if !ok {
		t.Fatal("issued ticket does not redeem")
	}
	if len(scopes) == 0 {
		t.Fatal("desktop ticket carries no scopes")
	}
}

func TestLocalEndpointTicketsAreDistinct(t *testing.T) {
	app := &App{tickets: newTicketStore(), hookPort: 1}
	if app.LocalEndpoint().Ticket == app.LocalEndpoint().Ticket {
		t.Fatal("two calls returned the same single-use ticket")
	}
}

// TestLocalEndpointGrantsTheUIAckScope pins the one issuer of scopeUIAck.
// ack_control_action is behind that scope precisely so a client that is not
// the in-process UI cannot answer a control verb on the UI's behalf; a ticket
// issued here that omitted it would break every UI-performed verb (spawn,
// focus_tab, tab_output) instead of failing visibly.
func TestLocalEndpointGrantsTheUIAckScope(t *testing.T) {
	app := &App{tickets: newTicketStore(), hookPort: 1}
	scopes, ok := app.tickets.redeem(app.LocalEndpoint().Ticket)
	if !ok {
		t.Fatal("issued ticket does not redeem")
	}
	for _, s := range scopes {
		if s == scopeUIAck {
			return
		}
	}
	t.Fatalf("the desktop's own ticket cannot ack a control action: %v", scopes)
}

// dialSeamWS is dialTestWS with the call seam replaced, so a command can be
// made to block on demand. No real App method blocks deterministically, and
// the two tests below are entirely about what happens while one call is slow.
func dialSeamWS(t *testing.T, call func(remoteCmd, map[string]json.RawMessage) (any, error)) *websocket.Conn {
	t.Helper()
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	h.call = call
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok := tickets.issue([]remoteScope{scopeOrchRead})
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatalf("welcome: %v", err)
	}
	return conn
}

// TestWSASlowCallDoesNotBlockTheNextOne is the head-of-line regression. serve()
// used to run inside the read loop, so the next ReadMessage waited for the
// current call to return — and the desktop drives the WHOLE app down one
// connection, so one generate_commit_message (a 180 s CLI budget) stalled
// every keystroke, create_pty and claude_send behind it.
func TestWSASlowCallDoesNotBlockTheNextOne(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	conn := dialSeamWS(t, func(c remoteCmd, _ map[string]json.RawMessage) (any, error) {
		if c.Method != "EnvironmentID" {
			return "fast", nil
		}
		entered <- struct{}{}
		<-release
		return "slow", nil
	})
	defer close(release)

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the slow call never started")
	}

	// Second call, sent while the first is still inside the seam.
	if err := conn.WriteJSON(clientFrame{T: "call", ID: 2, Cmd: "home_dir"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("the second call was head-of-line blocked by the first: %v", err)
	}
	if f.ID != 2 || f.Result != "fast" {
		t.Fatalf("want the fast reply first, got %+v", f)
	}
}

// TestWSTooManyInFlightCallsIsAnErrorNotAStall pins the other half of that
// fix: a goroutine per frame is only safe with a cap, and hitting the cap has
// to answer the id rather than park the read loop — parking it would be the
// same stall again, just later.
func TestWSTooManyInFlightCallsIsAnErrorNotAStall(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	release := make(chan struct{})
	entered := make(chan struct{}, maxInFlightCalls)
	conn := dialSeamWS(t, func(_ remoteCmd, _ map[string]json.RawMessage) (any, error) {
		entered <- struct{}{}
		<-release
		return nil, nil
	})
	defer close(release)

	for i := 1; i <= maxInFlightCalls; i++ {
		if err := conn.WriteJSON(clientFrame{T: "call", ID: int64(i), Cmd: "environment_id"}); err != nil {
			t.Fatal(err)
		}
	}
	// Every slot is taken once each seam call has been entered — the
	// semaphore is acquired in the read loop, before the goroutine starts.
	for i := 0; i < maxInFlightCalls; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d calls started", i, maxInFlightCalls)
		}
	}

	overflow := int64(maxInFlightCalls + 1)
	if err := conn.WriteJSON(clientFrame{T: "call", ID: overflow, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("the read loop stalled instead of refusing the call: %v", err)
	}
	if f.ID != overflow || f.Error == nil || f.Error.Code != "too_many_calls" {
		t.Fatalf("want a too_many_calls error for id %d, got %+v", overflow, f)
	}
}
