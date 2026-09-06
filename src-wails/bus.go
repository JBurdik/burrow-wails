package main

import "sync"

// EventSink receives every app event. The native window is one sink; the WS
// server (phase 4) is another.
type EventSink func(name string, payload any)

// ponytail: package-level bus. There is one app per process, and threading a
// handle through 11 call sites plus the daemon and hook goroutines buys
// nothing today. Make it injected when internal/server splits out.
type busEntry struct {
	id   uint64
	sink EventSink
}

var (
	busMu     sync.RWMutex
	busSinks  []busEntry
	busNextID uint64
)

// busSubscribe registers a sink and returns a function that removes it. The
// window sink is registered once for the app's lifetime and never removed; a
// remote connection's sink must be, or every connect leaks one.
func busSubscribe(s EventSink) func() {
	busMu.Lock()
	busNextID++
	id := busNextID
	busSinks = append(busSinks, busEntry{id: id, sink: s})
	busMu.Unlock()

	return func() {
		busMu.Lock()
		defer busMu.Unlock()
		for i, e := range busSinks {
			if e.id == id {
				last := len(busSinks) - 1
				copy(busSinks[i:], busSinks[i+1:])
				// Zero the now-unused tail slot explicitly: slicing alone
				// (append(s[:i], s[i+1:]...)) shifts elements left but
				// leaves a live copy of the closure in the backing array
				// past the new length, which the GC still treats as
				// reachable through that array — keeping a *websocket.Conn
				// and its 256-slot channel around until some later append
				// happens to overwrite that slot.
				busSinks[last] = busEntry{}
				busSinks = busSinks[:last]
				return
			}
		}
	}
}

// busEmit is the SINGLE door for any event a client may care about. There is
// deliberately no second path: the old emitAll had one, and the one call site
// that forgot it (emitWorkspacesChanged) meant the mobile client never learned
// that the workspace list had changed.
func busEmit(name string, payload any) {
	busMu.RLock()
	sinks := make([]EventSink, len(busSinks))
	for i, e := range busSinks {
		sinks[i] = e.sink
	}
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
