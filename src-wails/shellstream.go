package main

import "sync"

// shellRingSize is how many recent shell events a reconnecting client can
// catch up on. Beyond it the client is told to resync and takes a fresh
// snapshot — cheaper than any scheme that keeps per-client state on the
// server, which is what this deliberately avoids.
const shellRingSize = 512

// shellEvent is one bus event, numbered. The name and payload are exactly
// what busEmit carried, so the client's handlers are the same ones a live
// event goes through.
type shellEvent struct {
	Seq     int64  `json:"seq"`
	Name    string `json:"name"`
	Payload any    `json:"payload,omitempty"`
}

var (
	shellMu   sync.Mutex
	shellSeq  int64
	shellRing []shellEvent
)

// recordShellEvent numbers an event and files it in the ring.
func recordShellEvent(name string, payload any) shellEvent {
	shellMu.Lock()
	defer shellMu.Unlock()

	shellSeq++
	ev := shellEvent{Seq: shellSeq, Name: name, Payload: payload}

	shellRing = append(shellRing, ev)
	if len(shellRing) > shellRingSize {
		// Drop from the front. Copying keeps the slice's backing array from
		// growing without bound as it would with a bare reslice.
		copy(shellRing, shellRing[len(shellRing)-shellRingSize:])
		shellRing = shellRing[:shellRingSize]
	}
	return ev
}

// resumeSince returns the events after `since`. ok is false when the client's
// position is no longer in the ring — because it fell off the front, or
// because this process restarted and the numbering began again. Either way
// the client's next move is a snapshot, not a delta.
func resumeSince(since int64) ([]shellEvent, bool) {
	shellMu.Lock()
	defer shellMu.Unlock()

	// since 0 means the client holds nothing at all.
	if since <= 0 {
		return nil, false
	}
	if since > shellSeq {
		// Ahead of us: only possible across a restart.
		return nil, false
	}
	if len(shellRing) == 0 {
		return nil, since == shellSeq
	}
	oldest := shellRing[0].Seq
	if since < oldest-1 {
		return nil, false
	}

	out := make([]shellEvent, 0, shellSeq-since)
	for _, ev := range shellRing {
		if ev.Seq > since {
			out = append(out, ev)
		}
	}
	return out, true
}

func currentSeq() int64 {
	shellMu.Lock()
	defer shellMu.Unlock()
	return shellSeq
}

// shellStreamReset exists for tests.
func shellStreamReset() {
	shellMu.Lock()
	defer shellMu.Unlock()
	shellSeq = 0
	shellRing = nil
}
