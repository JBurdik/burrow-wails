package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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

func StartHookServer(ctx context.Context) (*HookServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	h := &HookServer{ctx: ctx, port: port}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", h.handleStatus)
	go http.Serve(ln, mux)

	return h, nil
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
		runtime.EventsEmit(h.ctx, eventName, p.State)
	case "error":
		runtime.EventsEmit(h.ctx, eventName, map[string]string{"state": "error", "detail": p.Detail})
	case "session":
		runtime.EventsEmit(h.ctx, eventName, map[string]string{"state": "session", "model": p.Model, "source": p.Source, "title": p.Title})
	default:
		runtime.EventsEmit(h.ctx, eventName, p.State)
	}

	w.WriteHeader(http.StatusOK)
}
