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
}

type frameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type serverFrame struct {
	T     string      `json:"t"`
	ID    int64       `json:"id,omitempty"`
	Result any        `json:"result,omitempty"`
	Error *frameError `json:"error,omitempty"`

	// event
	Name    string `json:"name,omitempty"`
	Payload any    `json:"payload,omitempty"`

	// welcome
	EnvironmentID string        `json:"environmentId,omitempty"`
	Scopes        []remoteScope `json:"scopes,omitempty"`
}

func decodeClientFrame(b []byte) (clientFrame, error) {
	var f clientFrame
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	// An unknown tag is rejected rather than ignored: silently dropping a
	// frame a future client sends is how a protocol mismatch turns into a
	// hang instead of an error.
	if f.T != "call" {
		return f, fmt.Errorf("unknown frame type %q", f.T)
	}
	// ID 0 would serialize out of a reply under omitempty and arrive
	// indistinguishable from an event, so the protocol refuses it at the door
	// rather than relying on every client to start counting at 1.
	if f.ID <= 0 {
		return f, fmt.Errorf("call frame needs a positive id, got %d", f.ID)
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

func welcomeFrame(envID string, scopes []remoteScope) serverFrame {
	return serverFrame{T: "welcome", EnvironmentID: envID, Scopes: scopes}
}
