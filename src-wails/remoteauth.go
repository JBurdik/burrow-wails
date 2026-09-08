package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// The pairing chain (spec §4):
//
//	POST /v2/pair       {code, name, kind}    → {device_token, environment_id, scopes}
//	POST /v2/ws-ticket  Authorization: Bearer → {ticket}
//	GET  /v2/ws?ticket=…
//
// Why three steps and not one. A device token is long-lived, so it must never
// travel in a URL — it would land in proxy logs, in browser history and in
// `tailscale serve` diagnostics. Browsers cannot set headers on a WebSocket
// handshake, so the handshake credential HAS to be in the query string; the
// answer is that it is a different credential, single-use and 30 s old at
// most, which is worthless by the time it reaches a log. That is the whole
// reason /v2/ws-ticket exists as its own hop.
//
// /v2/pair is unauthenticated by necessity — being able to pair without a
// credential is what pairing IS. What keeps it honest is that the code is six
// random digits, expires, is single-use, and the endpoint locks out after a
// handful of wrong guesses.

const (
	// pairCodeTTL — spec §4. A code shown in Settings in the morning must not
	// still work in the evening; a window measured in minutes is also what
	// makes the guess budget below meaningful rather than decorative.
	pairCodeTTL = 3 * time.Minute

	// pairCodeMaxFailures locks pairing after this many wrong codes. Six
	// digits against five guesses is 5-in-a-million, and this endpoint is
	// only reachable from the tailnet (funnel is refused at startup — see
	// remoteguard.go), but the endpoint cannot be authenticated, so the
	// attempt budget is the only thing standing there.
	pairCodeMaxFailures = 5
)

// remoteAuth owns the pairing code and the two HTTP hops above. It holds the
// app rather than a database handle because pairing writes a device row and
// reads the environment id, both of which live on App.
type remoteAuth struct {
	app     *App
	tickets *ticketStore

	mu       sync.Mutex
	code     string
	issuedAt time.Time
	failures int
}

func newRemoteAuth(app *App, tickets *ticketStore) *remoteAuth {
	a := &remoteAuth{app: app, tickets: tickets}
	a.rotate()
	return a
}

func (a *remoteAuth) register(mux *http.ServeMux) {
	mux.HandleFunc("/v2/pair", withCORS(a.handlePair))
	mux.HandleFunc("/v2/ws-ticket", withCORS(a.handleWSTicket))
}

// withCORS lets these two answer a caller on a different origin — the phone
// never needed this (it loads the PWA shell from this same host, so pairing
// is same-origin by construction), but the desktop app is its own origin
// (Wails' asset server, or wails:// in a built app) pairing OUT to some other
// Burrow's address, which makes this a genuinely cross-origin fetch. The
// security boundary here is the pairing code / device token, never the
// origin, so opening it is not weakening anything — same reasoning as the
// PWA shell and /healthz being unauthenticated on purpose.
func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// randomPairDigits returns six uniformly random digits. Deliberately NOT
// derived from any token: an earlier build showed the first six characters of
// the bearer token as a "pairing code", which leaked token material into the
// UI and authenticated nothing.
func randomPairDigits() string {
	digits := make([]byte, 6)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return ""
		}
		digits[i] = byte('0' + n.Int64())
	}
	return string(digits)
}

func (a *remoteAuth) rotate() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.code = randomPairDigits()
	a.issuedAt = time.Now()
	a.failures = 0
	return a.code
}

// PairStatus is what Settings renders: the code to type into the phone, when
// it dies, and whether the endpoint has locked itself out.
type PairStatus struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
	Locked    bool   `json:"locked"`
}

func (a *remoteAuth) status() PairStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	locked := a.failures >= pairCodeMaxFailures
	expired := time.Since(a.issuedAt) > pairCodeTTL
	st := PairStatus{
		ExpiresAt: a.issuedAt.Add(pairCodeTTL).UnixMilli(),
		Locked:    locked,
	}
	// An expired or locked-out code is reported as absent rather than as a
	// string that will not work: Settings showing digits that are already
	// dead is worse than Settings showing none.
	if !locked && !expired {
		st.Code = a.code
	}
	return st
}

// checkCode consumes an attempt. A correct code is single-use: success
// rotates it, so the same six digits cannot pair a second device.
func (a *remoteAuth) checkCode(got string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.failures >= pairCodeMaxFailures {
		return false
	}
	if a.code == "" || time.Since(a.issuedAt) > pairCodeTTL {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.code)) != 1 {
		a.failures++
		return false
	}
	// Rotate in place. Not via rotate(), which takes this same lock.
	a.code = randomPairDigits()
	a.issuedAt = time.Now()
	a.failures = 0
	return true
}

func (a *remoteAuth) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !a.checkCode(req.Code) {
		// One status for a wrong code, an expired code and a locked-out
		// endpoint: which of the three it was is information the caller has
		// not earned.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token, dev, err := a.app.pairDevice(req.Name, req.Kind)
	if err != nil {
		http.Error(w, "pairing failed", http.StatusInternalServerError)
		return
	}
	writeAuthJSON(w, map[string]any{
		"device_token":   token,
		"device_id":      dev.ID,
		"environment_id": a.app.environmentID,
		"scopes":         dev.Scopes,
	})
}

func (a *remoteAuth) handleWSTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Bearer only. A device token presented any other way — a query
	// parameter, the way the old /ws took one — is not read at all, which is
	// the invariant rather than a preference.
	tok := bearerToken(r)
	dev, ok := a.app.deviceForToken(tok)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	a.app.touchDevice(dev.ID)

	ticket := a.tickets.issueFor(dev.ID, dev.Scopes)
	if ticket == "" {
		http.Error(w, "could not issue a ticket", http.StatusInternalServerError)
		return
	}
	writeAuthJSON(w, map[string]any{"ticket": ticket, "environment_id": a.app.environmentID})
}

// bearerToken reads an Authorization: Bearer header, returning "" for anything
// else. It does not fall back to a query parameter or a cookie: the point of
// the ticket hop is that the long-lived credential never travels somewhere a
// log can keep it.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return ""
	}
	return h[len(prefix):]
}

// RemotePairStatus and RemoteRegeneratePairCode are the desktop's half of
// pairing. Both are access:* in remoteAllowed and therefore never reachable by
// a paired device — a device that could regenerate the code could pair another
// device, which is exactly what withholding access:write is for.
func (a *App) RemotePairStatus() PairStatus {
	if a.remoteAuth == nil {
		return PairStatus{}
	}
	return a.remoteAuth.status()
}

func (a *App) RemoteRegeneratePairCode() PairStatus {
	if a.remoteAuth == nil {
		return PairStatus{}
	}
	a.remoteAuth.rotate()
	return a.remoteAuth.status()
}

func writeAuthJSON(w http.ResponseWriter, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	// A pairing response carries a credential. Even on a tailnet, telling a
	// proxy it may keep it is free to avoid.
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
