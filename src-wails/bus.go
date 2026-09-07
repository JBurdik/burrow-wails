package main

import (
	"strings"
	"sync"
)

// EventSink receives every app event, carrying the ring's sequence number for
// the events that have one. Seq 0 means "not in the ring" — see isRingable;
// such an event is live-only and cannot be resumed.
type EventSink func(ev shellEvent)

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
	// Number it BEFORE fanning out, so every sink sees the same seq and the
	// ring's order is the order clients receive.
	ev := shellEvent{Name: name, Payload: payload}
	if isRingable(name) {
		ev = recordShellEvent(name, payload)
	}

	busMu.RLock()
	sinks := make([]EventSink, len(busSinks))
	for i, e := range busSinks {
		sinks[i] = e.sink
	}
	busMu.RUnlock()
	for _, s := range sinks {
		s(ev)
	}
}

// notRingable lists the event families the replay ring deliberately does not
// hold. Every one of them is BOTH high-volume and already replayable from a
// durable log of its own, which is the whole test for belonging on this list:
//
//	pty-data-*    the daemon keeps its own ring and replays it on reattach
//	claude-data-* ┐
//	acp-data-*    ├ persisted to chat_stream with an ord before they are ever
//	acp-req-*     │ emitted; replayChatStream/LoadChatEventsSince catch a
//	chat-event-*  ┘ client up from chat_stream_state.folded_ord
//
// Keeping them out is not only about the cost of ringing them — it is what
// makes resume work at all. The ring is 512 slots; one streaming agent emits
// hundreds of chat lines per turn, so ringing them would churn the whole
// buffer in seconds and every reconnect would come back to a resync instead
// of a delta. What is left in the ring is shell state — phases, workspaces,
// pty exits, control results — which changes a handful of times a minute, so
// 512 slots is hours of history rather than seconds.
var notRingable = []string{
	"pty-data-",
	"claude-data-",
	"acp-data-",
	"acp-req-",
	"chat-event-",
}

// isRingable reports whether an event belongs in the replay ring.
func isRingable(name string) bool {
	for _, p := range notRingable {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// busReset exists for tests.
func busReset() {
	busMu.Lock()
	defer busMu.Unlock()
	busSinks = nil
}
