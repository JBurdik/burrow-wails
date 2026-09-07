//go:build headless

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// The headless entrypoint for running Burrow as a standalone server — on a
// VPS, or anywhere else nobody wants a window. `App.startup` already takes
// nothing but a context.Context: no Wails call in it, no GUI dependency
// anywhere in the DB, hook server, phase store, remote auth or /v2/ws stack
// (that whole point of the remote-access rewrite was making every client,
// including the desktop's own window, a thin reader of this same backend —
// this file is what lets the backend exist without a desktop attached to it
// at all). What main.go's GUI build adds on top is exactly three things:
// the window itself, the menu bar, and saving/restoring window position —
// none of which mean anything with no window to have a position.
//
// Build with `-tags headless` (see justfile's build-server recipe): the
// `!headless` constraint on main.go excludes it and its wails/v2 GUI
// dependency from this build entirely, so cross-compiling for a Linux VPS
// (GOOS=linux CGO_ENABLED=0) never needs to link against GTK/webkit — the
// window toolkit's package is simply not part of the build graph.
func main() {
	app := NewApp()
	app.startup(context.Background())

	log.Printf("burrow server up (environment %s, hook port %d)", app.environmentID, app.hookPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Printf("shutting down")
	app.cleanupOnShutdown()
}
