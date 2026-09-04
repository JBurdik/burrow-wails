package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// The wire format is fixed by src/mobile/api.ts (call() and subscribe()).
// If these decodes drift, the mobile client silently stops working.
func TestWsCallDecode(t *testing.T) {
	var c wsCall
	if err := json.Unmarshal([]byte(`{"id":7,"command":"resize_pty","args":{"id":"pty-3","cols":80,"rows":24}}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != 7 || c.Command != "resize_pty" || c.Args.ID != "pty-3" || c.Args.Cols != 80 || c.Args.Rows != 24 {
		t.Fatalf("bad decode: %+v", c)
	}

	var sub wsCall
	if err := json.Unmarshal([]byte(`{"subscribe":"pty-data-3"}`), &sub); err != nil {
		t.Fatal(err)
	}
	if sub.Command != "" || sub.Subscribe != "pty-data-3" {
		t.Fatalf("subscribe frame must carry no command: %+v", sub)
	}

	var wr wsCall
	if err := json.Unmarshal([]byte(`{"id":1,"command":"write_pty","args":{"id":"p","data":[104,105]}}`), &wr); err != nil {
		t.Fatal(err)
	}
	if len(wr.Args.Data) != 2 || wr.Args.Data[0] != 104 {
		t.Fatalf("bad data decode: %+v", wr.Args)
	}
}

// Unknown commands must be rejected before anything touches the App —
// this surface is reachable from the tailnet.
func TestDispatchRejectsUnknown(t *testing.T) {
	s := &HTTPServer{}
	if _, err := s.dispatch(wsCall{Command: "kill_pty"}); err == nil {
		t.Fatal("expected an error for a command outside the allow-list")
	}
}

// The PWA only installs if the embedded bundle actually serves its shell,
// manifest and icons. Guards the //go:embed path and the "/" -> mobile.html
// mapping in one go. Skips when only the .gitkeep placeholder is embedded
// (i.e. `pnpm build:mobile` has not run).
func TestAssetsServeMobileShellAndManifest(t *testing.T) {
	s := &HTTPServer{}
	if rec := get(s, "/"); rec.Code == 404 {
		t.Skip("dist-mobile/app is empty — run `pnpm build:mobile`")
	}
	for path, want := range map[string]string{
		"/":                     "<div id=\"mobile-app\">",
		"/manifest.webmanifest": "\"display\": \"standalone\"",
	} {
		rec := get(s, path)
		if rec.Code != 200 {
			t.Fatalf("%s: got %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: body missing %q", path, want)
		}
	}
	if rec := get(s, "/icons/icon-192.png"); rec.Code != 200 {
		t.Fatalf("icon: got %d, want 200", rec.Code)
	}
	if rec := get(s, "/../httpserver.go"); rec.Code == 200 {
		t.Fatal("path traversal escaped the embedded FS")
	}
}

func get(s *HTTPServer, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleAssets(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The Connect screen sends whatever the user pastes straight to /ws?token=.
// A truncated value must be rejected: an earlier build showed a 6-char
// "pairing code" in Settings that the server never accepted, and the browser
// reports the resulting 401 only as "WebSocket connection failed".
func TestWSRejectsTruncatedToken(t *testing.T) {
	s := &HTTPServer{token: "0123456789abcdef", clients: map[*websocket.Conn]struct{}{}}
	srv := httptest.NewServer(http.HandlerFunc(s.handleWS))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	for _, tc := range []struct {
		name, token string
		wantOK      bool
	}{
		{"full token", "0123456789abcdef", true},
		{"first six chars", "012345", false},
		{"uppercased", "0123456789ABCDEF", false},
		{"empty", "", false},
	} {
		c, _, err := websocket.DefaultDialer.Dial(wsURL+"?token="+tc.token, nil)
		if err == nil {
			c.Close()
		}
		if gotOK := err == nil; gotOK != tc.wantOK {
			t.Errorf("%s: accepted=%v, want %v", tc.name, gotOK, tc.wantOK)
		}
	}
}

// Pairing is the only unauthenticated way to obtain the bearer token, so its
// three guarantees each get a case: right code works, a used code does not
// work twice, and guessing runs out of budget.
func TestPairing(t *testing.T) {
	s := &HTTPServer{token: "the-real-token", pairCode: "123456"}
	srv := httptest.NewServer(http.HandlerFunc(s.handlePair))
	defer srv.Close()

	pair := func(code string) (int, string) {
		res, err := http.Post(srv.URL, "application/json",
			strings.NewReader(`{"code":"`+code+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body.Token
	}

	if code, tok := pair("123456"); code != 200 || tok != "the-real-token" {
		t.Fatalf("correct code: got %d/%q, want 200/the-real-token", code, tok)
	}
	if code, _ := pair("123456"); code != 401 {
		t.Fatalf("reused code: got %d, want 401 — a paired code must be burned", code)
	}
	if s.PairCode() == "123456" {
		t.Fatal("code did not rotate after a successful pair")
	}

	// One failure is already on the counter from the reuse above.
	for i := 1; i < pairMaxFailures; i++ {
		pair("000000")
	}
	if code, _ := pair("000000"); code != 429 {
		t.Fatalf("after %d failures: got %d, want 429", pairMaxFailures, code)
	}
	if s.PairCode() != "" {
		t.Fatal("a locked-out server must not keep advertising a code")
	}
	// Even the right code stays refused while locked.
	live := s.pairCode
	if code, _ := pair(live); code != 429 {
		t.Fatalf("locked out but correct code: got %d, want 429", code)
	}
	if s.RegeneratePairCode() == "" || s.PairCode() == "" {
		t.Fatal("regenerate must unlock pairing")
	}
}

func TestRandomPairCodeIsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		c := randomPairCode()
		if len(c) != 6 {
			t.Fatalf("got %q, want 6 digits", c)
		}
		for _, r := range c {
			if r < '0' || r > '9' {
				t.Fatalf("non-digit in %q", c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 40 {
		t.Fatalf("only %d distinct codes in 50 draws — not random enough", len(seen))
	}
}

// A numeric id (what you get from JSON.stringify-ing a JS number, e.g.
// tab.ptyId in src/mobile/store.ts) makes the WHOLE wsCall fail to decode —
// not just the id field — so the command is silently dropped by the
// `if json.Unmarshal(...) != nil { continue }` guard in handleWS. Every
// mobile call site must therefore send ids as strings. This test exists so
// nobody "fixes" wsArgs.ID to accept numbers instead of fixing the caller.
func TestWsArgsRejectsNumericID(t *testing.T) {
	var c wsCall
	err := json.Unmarshal([]byte(`{"id":1,"command":"write_pty","args":{"id":42,"data":[1]}}`), &c)
	if err == nil {
		t.Fatal("expected a decode error for a numeric id — if this now passes, handleWS's silent-drop guard must be revisited too")
	}
}

// New write-path commands must decode their args and reach dispatch — this
// only checks the allow-list + arg shape, not the App call itself (that
// needs a live claudeMgr/acpReg, covered by claudechat_test.go/acp_test.go
// patterns instead).
func TestDispatchKnowsWritePathCommands(t *testing.T) {
	s := &HTTPServer{app: &App{}}
	for _, cmd := range []string{"claude_send", "acp_send", "claude_respond_control", "acp_respond_permission", "remote_create_chat"} {
		_, err := s.dispatch(wsCall{Command: cmd, Args: wsArgs{ID: "1"}})
		if err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%s: still not in the dispatch allow-list", cmd)
		}
	}
}

func TestWsArgsDecodesWritePathFields(t *testing.T) {
	var c wsCall
	raw := `{"id":1,"command":"claude_send","args":{"id":"3","text":"hi","sessionId":"abc"}}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.Args.Text != "hi" || c.Args.SessionId != "abc" {
		t.Fatalf("bad decode: %+v", c.Args)
	}

	var rc wsCall
	raw = `{"id":2,"command":"claude_respond_control","args":{"id":"3","requestId":"r1","response":{"behavior":"allow"}}}`
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	if rc.Args.RequestId != "r1" || rc.Args.Response["behavior"] != "allow" {
		t.Fatalf("bad decode: %+v", rc.Args)
	}

	var ap wsCall
	raw = `{"id":3,"command":"acp_respond_permission","args":{"id":"3","rpcId":42,"optionId":"allow_once"}}`
	if err := json.Unmarshal([]byte(raw), &ap); err != nil {
		t.Fatal(err)
	}
	if ap.Args.RpcId != 42 || ap.Args.OptionId != "allow_once" {
		t.Fatalf("bad decode: %+v", ap.Args)
	}
}
