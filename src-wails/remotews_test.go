package main

import (
	"encoding/json"
	"errors"
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
func dialTestWS(t *testing.T, app *App) (*websocket.Conn, serverFrame) {
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
	return conn, welcome
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
	// A ringable event arrives numbered, on the shell channel — that number
	// is the client's resume position, so an event delivered without one is
	// an event it can never ask for again.
	if f.T != "shell" || len(f.Events) != 1 {
		t.Fatalf("bad shell frame: %+v", f)
	}
	if f.Events[0].Name != "phase-pty:7" || f.Events[0].Seq <= 0 {
		t.Fatalf("bad shell event: %+v", f.Events[0])
	}
}

func TestWelcomeCarriesTheCurrentSeq(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	recordShellEvent("a", nil)
	recordShellEvent("b", nil)

	// Without this a reconnecting client's first resume is a guess, and a
	// first-time client has nothing to guess from at all.
	_, welcome := dialTestWS(t, &App{})
	if welcome.Seq != 2 {
		t.Fatalf("welcome seq %d, want 2", welcome.Seq)
	}
}

func TestResumeInsideTheRingSendsTheGap(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	conn, _ := dialTestWS(t, &App{})
	first := recordShellEvent("workspaces-changed", nil)
	second := recordShellEvent("phase-pty:7", map[string]string{"state": "running"})

	// The client says it holds `first`; the gap is everything after it.
	// Recorded directly rather than via busEmit so no live frame races the
	// resume reply onto the socket.
	if err := conn.WriteJSON(clientFrame{T: "resume", Since: first.Seq}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("no frame after resume: %v", err)
	}
	if f.T != "shell" {
		t.Fatalf("want a shell frame, got %q (%+v)", f.T, f)
	}
	if len(f.Events) != 1 || f.Events[0].Seq != second.Seq {
		t.Fatalf("want only the gap (seq %d), got %+v", second.Seq, f.Events)
	}
}

func TestResumeBeyondTheRingSendsResync(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	conn, _ := dialTestWS(t, &App{})
	for i := 0; i < shellRingSize+5; i++ {
		recordShellEvent("filler", nil)
	}

	// seq 1 fell off the front long ago. A partial answer would leave the
	// client silently missing events, so the only honest reply is "start
	// over from a snapshot".
	if err := conn.WriteJSON(clientFrame{T: "resume", Since: 1}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("no resync arrived: %v", err)
	}
	if f.T != "resync" {
		t.Fatalf("want resync, got %q (%+v)", f.T, f)
	}
}

func TestResumeFromNothingSendsResync(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	recordShellEvent("a", nil)

	conn, _ := dialTestWS(t, &App{})
	// since 0 is a first-time client. It has no position to catch up from,
	// so it needs a snapshot, not an empty delta it would mistake for
	// "already up to date".
	if err := conn.WriteJSON(clientFrame{T: "resume", Since: 0}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("no frame after resume: %v", err)
	}
	if f.T != "resync" {
		t.Fatalf("want resync, got %q (%+v)", f.T, f)
	}
}

func TestPtyDataIsNotInTheRing(t *testing.T) {
	// PTY bytes are the hot channel and the daemon replays them on reattach.
	// Ringing them would flush all 512 slots in a second of noisy output,
	// taking every phase change and workspace update with them.
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	busEmit("pty-data-7", []int{104, 105})
	if got := currentSeq(); got != 0 {
		t.Fatalf("pty-data was recorded: seq %d", got)
	}

	// Chat output is the other half of the rule: high volume AND already
	// replayable from chat_stream, so ringing it would churn all 512 slots
	// in one streaming turn and every reconnect would come back a resync.
	busEmit("claude-data-12", ChatStreamLine{Ord: 1, Kind: "claude-data", Line: "{}"})
	busEmit("chat-event-12", ChatEventBatch{Ord: 1})
	busEmit("acp-req-12", ChatStreamLine{Ord: 2, Kind: "acp-req", Line: "{}"})
	if got := currentSeq(); got != 0 {
		t.Fatalf("chat output was recorded: seq %d", got)
	}

	// Shell state is what the ring is for.
	busEmit("phase-pty:7", nil)
	busEmit("workspaces-changed", nil)
	if got := currentSeq(); got != 2 {
		t.Fatalf("ringable events were not recorded: seq %d", got)
	}
}

func TestPtyDataStaysOnTheUnnumberedChannel(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	conn, _ := dialTestWS(t, &App{})

	var f serverFrame
	overall := time.Now().Add(2 * time.Second)
	for {
		busEmit("pty-data-7", []int{104, 105})
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if err := conn.ReadJSON(&f); err == nil {
			break
		}
		if time.Now().After(overall) {
			t.Fatal("no frame arrived for pty-data")
		}
	}
	if f.T != "event" || f.Name != "pty-data-7" {
		t.Fatalf("pty-data must stay an unnumbered event frame, got %+v", f)
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

// seamWS starts a handler whose call seam is replaced, so a command can be
// made to block or panic on demand. No real App method does either
// deterministically, and the tests below are entirely about what happens
// while one call is slow or blows up. The returned dial opens a fresh
// connection to the same handler, which is how the panic test shows the
// process (and the handler) outlived the panicking call.
func seamWS(t *testing.T, call func(remoteCmd, map[string]json.RawMessage) (any, error)) func() *websocket.Conn {
	t.Helper()
	tickets := newTicketStore()
	h := newRemoteWS(&App{}, tickets)
	h.call = call
	mux := http.NewServeMux()
	h.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return func() *websocket.Conn {
		t.Helper()
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
}

func dialSeamWS(t *testing.T, call func(remoteCmd, map[string]json.RawMessage) (any, error)) *websocket.Conn {
	t.Helper()
	return seamWS(t, call)()
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
	// Wait until all 64 are actually inside the seam, so the cap is known to
	// be full before the overflow frame is sent. (This does not, and cannot,
	// distinguish acquiring the slot in the read loop from acquiring it
	// inside the goroutine — both reach the cap here. The read-loop
	// acquisition matters because it is what bounds the number of goroutines
	// spawned, not the number running, and that is an argument about the
	// code, not something this test observes.)
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

// TestWSAPanickingCallDropsTheConnectionNotTheProcess is the regression for
// dispatching calls on a bare goroutine. An exposed App method panicking is
// expected, not theoretical — several dereference a.daemon with no nil guard
// (remoteapi.go) — and a panic on a goroutine with no frame above it aborts
// the whole process, taking every terminal, chat and the window with it.
//
// The strong assertion is the second connection: reaching it at all means the
// test binary did not die, and getting a reply on it means the handler is
// still serving.
func TestWSAPanickingCallDropsTheConnectionNotTheProcess(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	dial := seamWS(t, func(c remoteCmd, _ map[string]json.RawMessage) (any, error) {
		if c.Method == "EnvironmentID" {
			panic("exposed method dereferenced a nil daemon")
		}
		return "fine", nil
	})

	doomed := dial()
	if err := doomed.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	// Either outcome is correct and which one lands is a race the recover
	// cannot win cleanly: it enqueues the error frame and then shuts the
	// connection down, so the writer may or may not flush before the socket
	// closes. What must NOT happen is a hang — or a dead process.
	_ = doomed.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f serverFrame
	switch err := doomed.ReadJSON(&f); {
	case err == nil:
		if f.ID != 1 || f.Error == nil || f.Error.Code != "call_failed" {
			t.Fatalf("want a call_failed reply for the panicking call, got %+v", f)
		}
	case isTimeout(err):
		t.Fatalf("the panicking call neither answered nor dropped the connection: %v", err)
	}

	// The process survived the panic; so should the handler.
	fresh := dial()
	if err := fresh.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "home_dir"}); err != nil {
		t.Fatal(err)
	}
	_ = fresh.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ok serverFrame
	if err := fresh.ReadJSON(&ok); err != nil {
		t.Fatalf("the handler stopped serving after a panicking call: %v", err)
	}
	if ok.Error != nil || ok.Result != "fine" {
		t.Fatalf("a fresh connection could not make a call: %+v", ok)
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
