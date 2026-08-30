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
