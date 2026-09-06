package main

import (
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
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("server did not drop the connection when its outbound queue filled")
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
