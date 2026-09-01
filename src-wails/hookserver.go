package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
)

// HookServer receives `burrow status <state>` POSTs from the `burrow`
// CLI (running inside spawned PTYs) and re-emits them as `pty-hook-{id}`
// events, matching src-tauri's start_hook_server / tiny_http implementation.
type HookServer struct {
	ctx  context.Context
	port int
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
func StartHookServer(ctx context.Context, routes ...func(*http.ServeMux)) (*HookServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	h := &HookServer{ctx: ctx, port: port}
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
		emitAll(h.ctx, "control:result", map[string]string{"token": p.Token})
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

	eventName := "pty-hook-" + p.PtyID
	switch p.State {
	case "waiting", "permission", "running", "done":
		emitAll(h.ctx, eventName, p.State)
	case "error":
		emitAll(h.ctx, eventName, map[string]string{"state": "error", "detail": p.Detail})
	case "session":
		emitAll(h.ctx, eventName, map[string]string{"state": "session", "model": p.Model, "source": p.Source, "title": p.Title})
	default:
		emitAll(h.ctx, eventName, p.State)
	}

	w.WriteHeader(http.StatusOK)
}
