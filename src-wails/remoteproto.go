package main

import (
	"encoding/json"
	"fmt"
)

// The /v2/ws wire format. Frames are tagged by `t` so the channel can carry
// calls, replies and events without the client guessing from shape.
//
// Event NAMES are identical to the bus event names (`pty-data-7`,
// `phase-pty:7`, `chat-event-12`) on purpose: no translation table means no
// translation table to forget an entry in.

type clientFrame struct {
	T    string                     `json:"t"`
	ID   int64                      `json:"id,omitempty"`
	Cmd  string                     `json:"cmd,omitempty"`
	Args map[string]json.RawMessage `json:"args,omitempty"`

	// resume
	Since int64 `json:"since,omitempty"`
}

type frameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type serverFrame struct {
	T      string      `json:"t"`
	ID     int64       `json:"id,omitempty"`
	Result any         `json:"result,omitempty"`
	Error  *frameError `json:"error,omitempty"`

	// event — the unnumbered channel: PTY bytes and chat output, which have
	// durable logs of their own (see notRingable in bus.go)
	Name    string `json:"name,omitempty"`
	Payload any    `json:"payload,omitempty"`

	// shell — numbered events: one when live, many when answering a resume
	Events []shellEvent `json:"events,omitempty"`

	// welcome
	EnvironmentID string        `json:"environmentId,omitempty"`
	Scopes        []remoteScope `json:"scopes,omitempty"`
	Seq           int64         `json:"seq,omitempty"`
}

func decodeClientFrame(b []byte) (clientFrame, error) {
	var f clientFrame
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	// An unknown tag is rejected rather than ignored: silently dropping a
	// frame a future client sends is how a protocol mismatch turns into a
	// hang instead of an error.
	switch f.T {
	case "call":
		// ID 0 would serialize out of a reply under omitempty and arrive
		// indistinguishable from an event, so the protocol refuses it at the
		// door rather than relying on every client to start counting at 1.
		// The rule belongs to `call` alone — a resume is not answered by id
		// and legitimately carries none.
		if f.ID <= 0 {
			return f, fmt.Errorf("call frame needs a positive id, got %d", f.ID)
		}
	case "resume":
		// since <= 0 is accepted, not rejected: it is how a client says "I
		// hold nothing", and resumeSince answers that with a resync.
	default:
		return f, fmt.Errorf("unknown frame type %q", f.T)
	}
	return f, nil
}

func replyFrame(id int64, result any) serverFrame {
	return serverFrame{T: "reply", ID: id, Result: result}
}

func errorFrame(id int64, code, msg string) serverFrame {
	return serverFrame{T: "reply", ID: id, Error: &frameError{Code: code, Message: msg}}
}

func eventFrame(name string, payload any) serverFrame {
	return serverFrame{T: "event", Name: name, Payload: payload}
}

// shellFrame carries numbered events: one when it is a live emit, many when
// it answers a resume.
func shellFrame(evs []shellEvent) serverFrame {
	return serverFrame{T: "shell", Events: evs}
}

// resyncFrame tells the client its position is gone and a fresh snapshot is
// the only way back. It deliberately carries no data: whatever the server
// would put here, shell_snapshot already returns, and two ways to rebuild the
// same state is one too many.
func resyncFrame() serverFrame { return serverFrame{T: "resync"} }

// welcomeFrame carries the seq the client resumes from if this connection
// drops. Without it a reconnecting client's first resume is a guess, and a
// first-time client has nothing to guess from at all.
func welcomeFrame(envID string, scopes []remoteScope, seq int64) serverFrame {
	return serverFrame{T: "welcome", EnvironmentID: envID, Scopes: scopes, Seq: seq}
}
