package main

import "sync"

// EventSink receives every app event. The native window is one sink; the WS
// server (phase 4) is another.
type EventSink func(name string, payload any)

// ponytail: package-level bus. There is one app per process, and threading a
// handle through 11 call sites plus the daemon and hook goroutines buys
// nothing today. Make it injected when internal/server splits out.
var (
	busMu    sync.RWMutex
	busSinks []EventSink
)

func busSubscribe(s EventSink) {
	busMu.Lock()
	defer busMu.Unlock()
	busSinks = append(busSinks, s)
}

// busEmit is the SINGLE door for any event a client may care about. There is
// deliberately no second path: the old emitAll had one, and the one call site
// that forgot it (emitWorkspacesChanged) meant the mobile client never learned
// that the workspace list had changed.
func busEmit(name string, payload any) {
	busMu.RLock()
	sinks := make([]EventSink, len(busSinks))
	copy(sinks, busSinks)
	busMu.RUnlock()
	for _, s := range sinks {
		s(name, payload)
	}
}

// busReset exists for tests.
func busReset() {
	busMu.Lock()
	defer busMu.Unlock()
	busSinks = nil
}
