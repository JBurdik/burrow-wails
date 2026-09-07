package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// authFixture wires the real pairing chain onto a test server: /v2/pair,
// /v2/ws-ticket and /v2/ws, all sharing one ticket store and one remoteWS —
// the same sharing the tailnet listener does, because a second store would
// mean a ticket nobody can redeem.
type authFixture struct {
	app  *App
	auth *remoteAuth
	ws   *remoteWS
	srv  *httptest.Server
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	app := newTestApp(t)
	app.environmentID = "env-test"
	app.tickets = newTicketStore()
	app.remoteWS = newRemoteWS(app, app.tickets)
	app.remoteAuth = newRemoteAuth(app, app.tickets)

	mux := http.NewServeMux()
	app.remoteWS.register(mux)
	app.remoteAuth.register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &authFixture{app: app, auth: app.remoteAuth, ws: app.remoteWS, srv: srv}
}

func (f *authFixture) pair(t *testing.T, code string) (*http.Response, map[string]any) {
	t.Helper()
	body := strings.NewReader(`{"code":"` + code + `","name":"phone","kind":"phone"}`)
	resp, err := http.Post(f.srv.URL+"/v2/pair", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// pairedToken pairs successfully and returns the device token.
func (f *authFixture) pairedToken(t *testing.T) string {
	t.Helper()
	resp, out := f.pair(t, f.auth.status().Code)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pairing failed: %d", resp.StatusCode)
	}
	tok, _ := out["device_token"].(string)
	if tok == "" {
		t.Fatalf("no device token in %v", out)
	}
	return tok
}

func (f *authFixture) wsTicket(t *testing.T, bearer string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/v2/ws-ticket", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var out struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out.Ticket
}

func TestPairingWithTheRightCodeYieldsADeviceToken(t *testing.T) {
	f := newAuthFixture(t)
	resp, out := f.pair(t, f.auth.status().Code)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if out["device_token"] == "" || out["device_token"] == nil {
		t.Fatalf("no device token: %v", out)
	}
	if out["environment_id"] != "env-test" {
		t.Fatalf("the phone needs the environment id to key its own state: %v", out)
	}
}

func TestPairCodeIsSingleUse(t *testing.T) {
	// A success rotates the code, so the digits on screen cannot pair a
	// second device behind the user's back.
	f := newAuthFixture(t)
	code := f.auth.status().Code
	if resp, _ := f.pair(t, code); resp.StatusCode != http.StatusOK {
		t.Fatalf("first pairing failed: %d", resp.StatusCode)
	}
	if resp, _ := f.pair(t, code); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the same code paired twice: %d", resp.StatusCode)
	}
}

func TestExpiredPairCodeIsRejected(t *testing.T) {
	// A code shown in Settings in the morning must not still work in the
	// evening — and the TTL is also what makes the guess budget meaningful.
	f := newAuthFixture(t)
	code := f.auth.status().Code

	f.auth.mu.Lock()
	f.auth.issuedAt = time.Now().Add(-pairCodeTTL - time.Second)
	f.auth.mu.Unlock()

	if resp, _ := f.pair(t, code); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an expired code paired: %d", resp.StatusCode)
	}
	// And Settings must not show digits that are already dead.
	if got := f.auth.status().Code; got != "" {
		t.Fatalf("status reported an expired code as usable: %q", got)
	}
}

func TestPairLocksOutAfterTheBudget(t *testing.T) {
	f := newAuthFixture(t)
	real := f.auth.status().Code
	for i := 0; i < pairCodeMaxFailures; i++ {
		if resp, _ := f.pair(t, "000000"); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("a wrong code was accepted on attempt %d", i)
		}
	}
	// Even the RIGHT code now fails: the endpoint cannot be authenticated,
	// so the budget is the only thing standing there and it has to hold even
	// against someone who has since learned the code.
	if resp, _ := f.pair(t, real); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pairing was not locked out: %d", resp.StatusCode)
	}
	if !f.auth.status().Locked {
		t.Fatal("status does not report the lockout, so Settings cannot offer a regenerate")
	}
	// A regenerate is the way out.
	f.auth.rotate()
	if resp, _ := f.pair(t, f.auth.status().Code); resp.StatusCode != http.StatusOK {
		t.Fatalf("a regenerated code did not clear the lockout: %d", resp.StatusCode)
	}
}

func TestWsTicketRequiresABearerDeviceToken(t *testing.T) {
	f := newAuthFixture(t)
	if resp, _ := f.wsTicket(t, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a ticket was issued with no credential: %d", resp.StatusCode)
	}
	if resp, _ := f.wsTicket(t, "not-a-device-token"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a ticket was issued to an unknown token: %d", resp.StatusCode)
	}
	tok := f.pairedToken(t)
	resp, ticket := f.wsTicket(t, tok)
	if resp.StatusCode != http.StatusOK || ticket == "" {
		t.Fatalf("a paired device could not get a ticket: %d %q", resp.StatusCode, ticket)
	}
}

func TestADeviceTokenInTheQueryStringIsNotAccepted(t *testing.T) {
	// The invariant that has to survive a refactor (spec §4 invariant 3): the
	// long-lived credential never travels somewhere a proxy log keeps it. The
	// old /ws took its token exactly this way; /v2 must not.
	f := newAuthFixture(t)
	tok := f.pairedToken(t)

	resp, err := http.Post(f.srv.URL+"/v2/ws-ticket?token="+tok, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a device token in the query string was accepted: %d", resp.StatusCode)
	}

	// Nor may it work as a ticket on the socket itself.
	url := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + tok
	if conn, _, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		conn.Close()
		t.Fatal("a device token was accepted as a ws ticket")
	}
}

func TestATicketIsGoodForExactlyOneHandshake(t *testing.T) {
	f := newAuthFixture(t)
	t.Cleanup(busReset)
	busReset()

	tok := f.pairedToken(t)
	_, ticket := f.wsTicket(t, tok)
	url := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + ticket

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("first handshake failed: %v", err)
	}
	defer conn.Close()

	if second, _, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
		second.Close()
		t.Fatal("the same ticket authorized a second connection")
	}
}

func TestRevokedDeviceCannotGetATicket(t *testing.T) {
	f := newAuthFixture(t)
	tok := f.pairedToken(t)
	devs, err := f.app.RemoteDevices()
	if err != nil || len(devs) != 1 {
		t.Fatalf("expected one paired device: %v %v", devs, err)
	}
	if err := f.app.RevokeRemoteDevice(devs[0].ID); err != nil {
		t.Fatal(err)
	}
	if resp, _ := f.wsTicket(t, tok); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a revoked device still gets tickets: %d", resp.StatusCode)
	}
}

func TestAPairedDeviceGetsTheScopesItsRowSays(t *testing.T) {
	// Spec §7 test 8: a session without access:write cannot reach a pairing
	// verb. Enforced per command by remoteWS.serve, so this asserts the
	// ticket's own scopes rather than trusting the ticket to be narrow.
	f := newAuthFixture(t)
	tok := f.pairedToken(t)
	_, ticket := f.wsTicket(t, tok)

	tk, ok := f.app.tickets.redeem(ticket)
	if !ok {
		t.Fatal("the issued ticket does not redeem")
	}
	if tk.deviceID == "" {
		t.Fatal("a device's ticket carries no device id, so a revoke cannot find its sockets")
	}
	for _, s := range tk.scopes {
		if s == scopeAccessWrite || s == scopeUIAck {
			t.Fatalf("a paired device's ticket carries %s", s)
		}
	}
}

func TestPairedDeviceIsRefusedTheAccessWriteCommands(t *testing.T) {
	// The end-to-end version of the scope check: a real connection, made
	// with a real paired device's ticket, calling a real access:write verb.
	f := newAuthFixture(t)
	t.Cleanup(busReset)
	busReset()

	tok := f.pairedToken(t)
	_, ticket := f.wsTicket(t, tok)
	url := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + ticket
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "remote_regenerate_pair_code"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f2 serverFrame
	if err := conn.ReadJSON(&f2); err != nil {
		t.Fatal(err)
	}
	if f2.Error == nil || f2.Error.Code != "forbidden" {
		t.Fatalf("a paired device was allowed to regenerate the pairing code: %+v", f2)
	}
}

func TestRevokingADeviceDropsItsLiveConnection(t *testing.T) {
	// A revoke that leaves yesterday's socket running is not a revoke, and
	// that socket is the whole app.
	f := newAuthFixture(t)
	t.Cleanup(busReset)
	busReset()

	tok := f.pairedToken(t)
	_, ticket := f.wsTicket(t, tok)
	url := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + ticket
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var welcome serverFrame
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatal(err)
	}

	devs, _ := f.app.RemoteDevices()
	if err := f.app.RevokeRemoteDevice(devs[0].ID); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the revoked device's socket is still open")
	} else if isTimeout(err) {
		t.Fatalf("the socket stayed open; the read merely timed out: %v", err)
	}
}

func TestRevokingOneDeviceLeavesAnotherConnected(t *testing.T) {
	f := newAuthFixture(t)
	t.Cleanup(busReset)
	busReset()

	// Two devices, two sockets.
	tokA := f.pairedToken(t)
	f.auth.rotate()
	tokB := f.pairedToken(t)

	dial := func(deviceToken string) *websocket.Conn {
		_, ticket := f.wsTicket(t, deviceToken)
		url := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket=" + ticket
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		var w serverFrame
		if err := c.ReadJSON(&w); err != nil {
			t.Fatal(err)
		}
		return c
	}
	connA, connB := dial(tokA), dial(tokB)

	devA, _ := f.app.deviceForToken(tokA)
	if err := f.app.RevokeRemoteDevice(devA.ID); err != nil {
		t.Fatal(err)
	}

	_ = connA.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := connA.ReadMessage(); err == nil || isTimeout(err) {
		t.Fatalf("the revoked device's socket survived: %v", err)
	}

	// B must still be able to work. A revoke that takes the whole listener
	// down with it would look like a success in the test above.
	if err := connB.WriteJSON(clientFrame{T: "call", ID: 1, Cmd: "environment_id"}); err != nil {
		t.Fatal(err)
	}
	_ = connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	var reply serverFrame
	if err := connB.ReadJSON(&reply); err != nil {
		t.Fatalf("the other device's connection broke: %v", err)
	}
	if reply.Result != "env-test" {
		t.Fatalf("the other device got a bad reply: %+v", reply)
	}
}
