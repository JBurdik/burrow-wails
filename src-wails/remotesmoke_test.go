package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestRemoteSmoke walks the whole path a phone takes, end to end over loopback:
// pair → ticket → socket → calls → events + resume → revoke. The pieces have
// their own focused tests; this one pins that they still compose, on one real
// device, through the real handlers (`just smoke-remote`).
func TestRemoteSmoke(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	f := newAuthFixture(t)
	wsURL := strings.Replace(f.srv.URL, "http://", "ws://", 1) + "/v2/ws?ticket="

	dial := func(t *testing.T, ticket string) *websocket.Conn {
		t.Helper()
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL+ticket, nil)
		if err != nil {
			t.Fatalf("dial: %v (%v)", err, resp)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	ticketFor := func(t *testing.T, tok string) string {
		t.Helper()
		resp, ticket := f.wsTicket(t, tok)
		if resp.StatusCode != http.StatusOK || ticket == "" {
			t.Fatalf("ws-ticket: %d %q", resp.StatusCode, ticket)
		}
		return ticket
	}
	call := func(t *testing.T, c *websocket.Conn, id int64, cmd string, args map[string]any) serverFrame {
		t.Helper()
		raw := map[string]json.RawMessage{}
		for k, v := range args {
			raw[k], _ = json.Marshal(v)
		}
		if err := c.WriteJSON(clientFrame{T: "call", ID: id, Cmd: cmd, Args: raw}); err != nil {
			t.Fatal(err)
		}
		return readUntil(t, c, cmd+" reply", func(fr serverFrame) bool { return fr.T == "reply" && fr.ID == id })
	}

	// 1. Pairing: a wrong guess counts, the right code pairs, the code is then spent.
	code := f.auth.status().Code
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if resp, _ := f.pair(t, wrong); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d, want 401", resp.StatusCode)
	}
	if f.auth.failures != 1 {
		t.Fatalf("wrong guess not counted toward lockout: failures=%d", f.auth.failures)
	}
	resp, out := f.pair(t, code)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair: %d", resp.StatusCode)
	}
	tokA, _ := out["device_token"].(string)
	devA, _ := out["device_id"].(string)
	if tokA == "" || devA == "" || out["environment_id"] != "env-test" {
		t.Fatalf("pair reply: %v", out)
	}
	if resp, _ := f.pair(t, code); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused code: %d, want 401", resp.StatusCode)
	}

	// 2. Ticket: Bearer only, single use.
	if resp, _ := f.wsTicket(t, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer: %d, want 401", resp.StatusCode)
	}
	if resp, _ := f.wsTicket(t, "not-a-token"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad bearer: %d, want 401", resp.StatusCode)
	}
	ticket := ticketFor(t, tokA)

	// 3. Socket: welcome carries the protocol revision and the stream position.
	busEmit("workspaces-changed", nil) // so the welcome has a non-zero seq to report
	conn := dial(t, ticket)
	welcome := readUntil(t, conn, "welcome", func(fr serverFrame) bool { return true })
	if welcome.T != "welcome" || welcome.Rev != protocolRev || welcome.MinClientRev != minClientRev || welcome.Seq != currentSeq() {
		t.Fatalf("welcome: %+v (want rev %d/%d seq %d)", welcome, protocolRev, minClientRev, currentSeq())
	}
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL+ticket, nil); err == nil {
		t.Fatal("a ticket redeemed twice")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second redeem: want 401, got %v (%v)", resp, err)
	}

	// 4. Calls: in scope → reply; out of scope → forbidden; unknown → unknown_command
	// (transport.ts's wake probe counts any reply, this one included, as alive).
	if fr := call(t, conn, 1, "environment_id", nil); fr.Error != nil || fr.Result != "env-test" {
		t.Fatalf("in-scope call: %+v", fr)
	}
	if fr := call(t, conn, 2, "remote_devices", nil); fr.Error == nil || fr.Error.Code != "forbidden" {
		t.Fatalf("out-of-scope call: %+v", fr)
	}
	if fr := call(t, conn, 3, "__probe", nil); fr.Error == nil || fr.Error.Code != "unknown_command" {
		t.Fatalf("unknown command: %+v", fr)
	}

	// 5. Events live, a drop mid-stream, resume on a new socket = exactly the gap.
	busEmit("phase-pty:7", map[string]string{"state": "running"})
	busEmit("phase-pty:7", map[string]string{"state": "waiting_input"})
	var last int64
	for got := 0; got < 2; {
		fr := readUntil(t, conn, "live event", func(fr serverFrame) bool { return fr.T == "shell" })
		for _, ev := range fr.Events {
			if ev.Seq <= last {
				t.Fatalf("live event out of order / duplicated: %d after %d", ev.Seq, last)
			}
			last = ev.Seq
			got++
		}
	}
	conn.Close()

	busEmit("phase-pty:7", map[string]string{"state": "done"})
	busEmit("pty-data-7", map[string]any{"off": 0, "data": "hi"}) // not ringed: never in a resume
	busEmit("workspaces-changed", nil)
	missed := currentSeq()

	conn = dial(t, ticketFor(t, tokA))
	readUntil(t, conn, "welcome", func(fr serverFrame) bool { return fr.T == "welcome" })
	if err := conn.WriteJSON(clientFrame{T: "resume", Since: last}); err != nil {
		t.Fatal(err)
	}
	fr := readUntil(t, conn, "resume", func(fr serverFrame) bool { return fr.T == "shell" || fr.T == "resync" })
	if fr.T != "shell" {
		t.Fatalf("resume inside the ring answered %q", fr.T)
	}
	var names []string
	prev := last
	for _, ev := range fr.Events {
		if ev.Seq <= prev {
			t.Fatalf("resume repeated or reordered seq %d after %d", ev.Seq, prev)
		}
		prev = ev.Seq
		names = append(names, ev.Name)
	}
	if strings.Join(names, ",") != "phase-pty:7,workspaces-changed" || prev != missed {
		t.Fatalf("resume delivered %v up to seq %d, want the two missed shell events up to %d", names, prev, missed)
	}

	// A position older than the ring can only be answered with a resync.
	for i := 0; i < shellRingSize+5; i++ {
		recordShellEvent("filler", nil)
	}
	if err := conn.WriteJSON(clientFrame{T: "resume", Since: last}); err != nil {
		t.Fatal(err)
	}
	if fr := readUntil(t, conn, "resync", func(fr serverFrame) bool { return fr.T == "shell" || fr.T == "resync" }); fr.T != "resync" {
		t.Fatalf("resume past the ring answered %q, want resync", fr.T)
	}

	// 7. push_subscribe_apns acts as the calling device whatever `_device` it names.
	_, outB := f.pair(t, f.auth.status().Code)
	devB, _ := outB["device_id"].(string)
	if devB == "" || devB == devA {
		t.Fatalf("second device: %v", outB)
	}
	apnsTok := strings.Repeat("ab", 32)
	if fr := call(t, conn, 4, "push_subscribe_apns", map[string]any{deviceArg: devB, "token": apnsTok, "sandbox": true}); fr.Error != nil {
		t.Fatalf("push_subscribe_apns: %+v", fr.Error)
	}
	var owner string
	if err := f.app.db.QueryRow(`SELECT device_id FROM apns_tokens WHERE token = ?`, apnsTok).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != devA {
		t.Fatalf("APNs token filed under %q, want the calling device %q (it named %q)", owner, devA, devB)
	}

	// 6. Revoke: the live socket closes, the token gets no more tickets.
	if err := f.app.RevokeRemoteDevice(devA); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if isTimeout(err) {
				t.Fatalf("revoked device's socket stayed open: %v", err)
			}
			break
		}
	}
	if resp, _ := f.wsTicket(t, tokA); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token got a ticket: %d", resp.StatusCode)
	}

	// Lockout: after the failure budget even the right code is refused.
	code = f.auth.status().Code
	wrong = "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < pairCodeMaxFailures; i++ {
		f.pair(t, wrong)
	}
	if resp, _ := f.pair(t, code); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("right code after lockout: %d, want 401", resp.StatusCode)
	}
}
