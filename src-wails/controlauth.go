package main

import (
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"burrow/internal/control"
	"burrow/internal/ctlauth"
)

// Loopback control auth (internal/ctlauth has the wire format). A client never
// sends control.token: it answers a single-use challenge, and the core proves
// itself back in X-Burrow-Proof, so a process holding the port while the core
// is down learns nothing. A chat / tab signs with its own caller key
// (BURROW_CALLER_TOKEN = ctlauth.CallerKey(token, "chat:<id>" | "pty:<id>")),
// which is who the call is from — not the BURROW_CHAT_ID it also sends.
//
// ponytail: the ceiling is control.token itself (0600, same user). An agent
// that may run any command (full access) can read it and act as the master
// key; caller keys only bind agents whose own permission mode stops them from
// reading files outside their project unasked.

// ctlCaller is who signed a call. Kind "" is the master token (the user's own
// shell, the app, an old Bearer client): full authority, hints trusted.
type ctlCaller struct {
	Kind string // "", "chat" or "pty"
	ID   int64
}

const ctlNonceTTL = 30 * time.Second

var ctlNonces = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// handleControlChallenge: POST /v1/_challenge → a fresh single-use nonce.
func (a *App) handleControlChallenge(w http.ResponseWriter, r *http.Request) {
	n := ctlauth.Nonce()
	now := time.Now()
	ctlNonces.Lock()
	for k, t := range ctlNonces.m {
		if now.Sub(t) > ctlNonceTTL {
			delete(ctlNonces.m, k)
		}
	}
	full := len(ctlNonces.m) >= 4096
	if !full && n != "" {
		ctlNonces.m[n] = now
	}
	ctlNonces.Unlock()
	if full || n == "" {
		http.Error(w, "too many open challenges", http.StatusServiceUnavailable)
		return
	}
	writeJSONFileResponse(w, map[string]string{"nonce": n})
}

func takeCtlNonce(n string) bool {
	ctlNonces.Lock()
	defer ctlNonces.Unlock()
	t, ok := ctlNonces.m[n]
	delete(ctlNonces.m, n)
	return ok && time.Since(t) <= ctlNonceTTL
}

// localPort is the port this request came in on — the one the client dialed.
func localPort(r *http.Request) int {
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if tcp, ok := addr.(*net.TCPAddr); ok {
			return tcp.Port
		}
	}
	return 0
}

var lastBearerLog atomic.Int64

// controlAuth checks a loopback call and, when it passes, sets the core's own
// proof on the response. Fails closed without a token.
func (a *App) controlAuth(w http.ResponseWriter, r *http.Request) (ctlCaller, bool) {
	if a.controlToken == "" {
		return ctlCaller{}, false
	}
	h := r.Header.Get("Authorization")
	// DEPRECATED: Bearer sends the token itself to whoever holds the port.
	// Accepted for one release so already installed CLIs / hooks / plugins keep
	// working. Remove after 2027-01-31.
	if strings.HasPrefix(h, "Bearer ") {
		if subtle.ConstantTimeCompare([]byte(h), []byte("Bearer "+a.controlToken)) != 1 {
			return ctlCaller{}, false
		}
		if now := time.Now().Unix(); now-lastBearerLog.Load() > 600 {
			lastBearerLog.Store(now)
			log.Printf("control: deprecated Bearer auth on %s (an old burrow CLI / burrow-mcp); it stops working after 2027-01-31", r.URL.Path)
		}
		return ctlCaller{}, true
	}
	sn, cn, proof, caller, ok := ctlauth.Parse(h)
	if !ok || !takeCtlNonce(sn) || len(cn) < 32 {
		return ctlCaller{}, false
	}
	key, who := a.controlToken, ctlCaller{}
	if caller != "" {
		kind, id, _ := strings.Cut(caller, ":")
		n, err := strconv.ParseInt(id, 10, 64)
		if (kind != "chat" && kind != "pty") || err != nil || n <= 0 {
			return ctlCaller{}, false
		}
		key, who = ctlauth.CallerKey(a.controlToken, caller), ctlCaller{Kind: kind, ID: n}
	}
	port := localPort(r)
	if !ctlauth.Equal(proof, ctlauth.Proof(key, "client", sn, cn, port)) {
		return ctlCaller{}, false
	}
	w.Header().Set(ctlauth.ProofHeader, ctlauth.Proof(key, "server", sn, cn, port))
	return who, true
}

// callerKeyEnv is the BURROW_CALLER_TOKEN a chat ("chat") or tab ("pty")
// process gets; "" without a token.
func (a *App) callerKeyEnv(kind string, id int64) string {
	if a.controlToken == "" || id <= 0 {
		return ""
	}
	return ctlauth.CallerKey(a.controlToken, kind+":"+strconv.FormatInt(id, 10))
}

// bindCaller makes the signed identity the call's identity: the hints a client
// sends (parent_chat_id / caller_pty_id) must agree with the key, and are then
// overwritten with it. A master-key call keeps its hints.
func bindCaller(who ctlCaller, p control.Params) string {
	if who.Kind == "" {
		return ""
	}
	own, other := "parent_chat_id", "caller_pty_id"
	if who.Kind == "pty" {
		own, other = other, own
	}
	if v, ok := p[own]; ok && v != nil && v != "" && p.Int(own) != who.ID {
		return "this call is signed by " + who.Kind + " " + strconv.FormatInt(who.ID, 10) + " but names another caller in " + own
	}
	if v, ok := p[other]; ok && v != nil && v != "" {
		return "this call is signed by " + who.Kind + " " + strconv.FormatInt(who.ID, 10) + " but also names " + other
	}
	p[own] = who.ID
	return ""
}
