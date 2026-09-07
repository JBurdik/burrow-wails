package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What this file used to test is gone: wsCall/wsArgs decoding, the
// hand-written dispatch, the shared-token /ws auth and the /pair that handed
// that one token to every device. Phase 6 deleted the surface along with the
// client that spoke it (src/mobile/api.ts). The pairing chain that replaced it
// is covered in remoteauth_test.go.
//
// What is left here is what the listener still owns: the PWA bundle, the
// health probe, and the fact that the v1 paths are actually gone rather than
// merely unused.

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

// TestV1SurfaceIsGone pins the deletion rather than trusting that nothing
// calls it. A route left mounted is a route someone can reach: /ws took a
// long-lived token in a query parameter, and /pair handed that same token to
// every device that asked. Both are exactly what phase 5's model replaced.
func TestV1SurfaceIsGone(t *testing.T) {
	app := &App{tickets: newTicketStore()}
	app.remoteWS = newRemoteWS(app, app.tickets)
	app.remoteAuth = newRemoteAuth(app, app.tickets)

	srv := httptest.NewServer(NewHTTPServer(app).mux())
	t.Cleanup(srv.Close)

	// With an empty dist-mobile every path 404s and this test would pass for
	// the wrong reason, so require the bundle to actually be serving first.
	if rec := get(&HTTPServer{}, "/"); rec.Code == 404 {
		t.Skip("dist-mobile/app is empty — run `pnpm build:mobile`")
	}

	for _, path := range []string{"/ws", "/rpc/create-pty", "/pair"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resp.Body.Close()
		// Not 200, and not 401 either: 401 would mean the handler is still
		// mounted and merely refusing this caller.
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s is still served (%d); the v1 surface must be gone, not guarded", path, resp.StatusCode)
		}
	}
}

func TestHealthAndV2SurviveTheCutover(t *testing.T) {
	app := &App{tickets: newTicketStore()}
	app.remoteWS = newRemoteWS(app, app.tickets)
	app.remoteAuth = newRemoteAuth(app, app.tickets)

	srv := httptest.NewServer(NewHTTPServer(app).mux())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("/healthz: got %d, want 200", resp.StatusCode)
	}

	// /v2/ws with no ticket must be a 401 — mounted and refusing — which is
	// the opposite of what the test above requires of the v1 paths.
	resp, err = http.Get(srv.URL + "/v2/ws")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/v2/ws with no ticket: got %d, want 401", resp.StatusCode)
	}

	// /v2/pair is mounted and answers POST-only; a GET proves it exists
	// without needing a code.
	resp, err = http.Get(srv.URL + "/v2/pair")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("/v2/pair GET: got %d, want 405", resp.StatusCode)
	}
}
