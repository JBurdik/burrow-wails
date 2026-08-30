package main

import (
	"context"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// wsBroadcaster points at the live HTTPServer while remote access is on,
// nil otherwise. Set by SetHttpEnabled; read from the daemon/hook
// goroutines, hence the atomic.
var wsBroadcaster atomic.Pointer[HTTPServer]

// emitAll emits to the native Wails window AND (when remote access is on)
// to every connected browser client. The Rust backend called this
// emit_all; same idea. Use it for any event the mobile UI subscribes to.
func emitAll(ctx context.Context, event string, payload any) {
	runtime.EventsEmit(ctx, event, payload)
	if s := wsBroadcaster.Load(); s != nil {
		s.Broadcast(event, payload)
	}
}

// Global (no-suffix) events, matching src-tauri/src/lib.rs's emit_all calls.
func emitWorkspacesChanged(ctx context.Context) { runtime.EventsEmit(ctx, "workspaces-changed") }
