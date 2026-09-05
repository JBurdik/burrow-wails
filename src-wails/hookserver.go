package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"

	"burrow/internal/agentphase"
)

// HookServer receives `burrow status <state>` POSTs from the `burrow`
// CLI (running inside spawned PTYs) and re-emits them as `pty-hook-{id}`
// events, matching src-tauri's start_hook_server / tiny_http implementation.
type HookServer struct {
	ctx      context.Context
	port     int
	mu       sync.RWMutex
	statuses map[string]hookPayload
	phases   *PhaseStore
}

type hookPayload struct {
	PtyID  string `json:"ptyId"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Model  string `json:"model,omitempty"`
	Source string `json:"source,omitempty"`
	Title  string `json:"title,omitempty"`
}

// StartHookServer listens on a loopback ephemeral port. Callers pass registrars
// for the other loopback routes (the control API) so everything an agent's shell
// needs lives behind one port + one port file.
func StartHookServer(ctx context.Context, phases *PhaseStore, routes ...func(*http.ServeMux)) (*HookServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	h := &HookServer{ctx: ctx, port: port, statuses: make(map[string]hookPayload), phases: phases}
	mux := http.NewServeMux()
	// /hook is the path the `burrow` CLI has always posted to; /status is kept as
	// an alias. Serving only /status silently broke every status dot: the CLI's
	// `curl -sf` failed on the 404 and exited 0, so a lost state looked like an
	// agent that never reported.
	mux.HandleFunc("/hook", h.handleStatus)
	mux.HandleFunc("/status", h.handleStatus)
	// Posted by `burrow capture` once a sub-agent's result file is written, so a
	// Manager can collect immediately instead of polling.
	mux.HandleFunc("/agent-done", h.handleAgentDone)
	for _, register := range routes {
		register(mux)
	}
	go http.Serve(ln, mux)

	return h, nil
}

// handleAgentDone re-emits a finished sub-agent as an app event. The token is
// all the payload carries; the result itself is read with collect_results.
func (h *HookServer) handleAgentDone(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var p struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	if p.Token != "" {
		busEmit("control:result", map[string]string{"token": p.Token})
	}
	w.WriteHeader(http.StatusOK)
}

func (h *HookServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var p hookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// A terminal thread can already be running before its XTerm view attaches.
	// Keep the latest state and replay it from CreatePty once that view has its
	// listener installed; otherwise the initial `running` hook is lost and the
	// sidebar mirror remains idle until the next hook arrives.
	if p.PtyID != "" && p.State != "session" {
		h.mu.Lock()
		h.statuses[p.PtyID] = p
		h.mu.Unlock()
	}
	h.emitStatus(p)

	if h.phases != nil && p.PtyID != "" {
		if ev, ok := hookEvent(p); ok {
			h.phases.Apply("pty:"+p.PtyID, ev)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// hookEvent translates one `burrow status` POST into a phase event. An unknown
// state returns false: a hook nobody planned for must not move the dot.
func hookEvent(p hookPayload) (agentphase.Event, bool) {
	switch p.State {
	case "running":
		return agentphase.Event{Kind: agentphase.HookRunning}, true
	case "waiting":
		return agentphase.Event{Kind: agentphase.HookWaiting}, true
	case "permission":
		return agentphase.Event{Kind: agentphase.HookPermission}, true
	case "done":
		return agentphase.Event{Kind: agentphase.HookDone}, true
	case "error":
		return agentphase.Event{Kind: agentphase.HookError, Detail: p.Detail}, true
	case "session":
		return agentphase.Event{Kind: agentphase.HookSession, Model: p.Model, Source: p.Source, Title: p.Title}, true
	}
	return agentphase.Event{}, false
}

// ForgetStatus drops a PTY's cached hook state. Its pair is ReplayStatus:
// a pty id that has just been reused by a FRESH spawn must not replay the
// status of the session that held the id before it, on the legacy
// pty-hook-{id} channel any more than on the phase one.
func (h *HookServer) ForgetStatus(ptyID string) {
	h.mu.Lock()
	delete(h.statuses, ptyID)
	h.mu.Unlock()
}

// ReplayStatus re-emits a PTY's last hook state after a frontend attaches.
// The caller creates the PTY only after XTerm has subscribed to pty-hook-{id}.
func (h *HookServer) ReplayStatus(ptyID string) {
	h.mu.RLock()
	p, ok := h.statuses[ptyID]
	h.mu.RUnlock()
	if ok {
		h.emitStatus(p)
	}
	if h.phases != nil {
		h.phases.Replay("pty:" + ptyID)
	}
}

func (h *HookServer) emitStatus(p hookPayload) {
	eventName := "pty-hook-" + p.PtyID
	switch p.State {
	case "waiting", "permission", "running", "done":
		busEmit(eventName, p.State)
	case "error":
		busEmit(eventName, map[string]string{"state": "error", "detail": p.Detail})
	case "session":
		busEmit(eventName, map[string]string{"state": "session", "model": p.Model, "source": p.Source, "title": p.Title})
	default:
		busEmit(eventName, p.State)
	}
}
