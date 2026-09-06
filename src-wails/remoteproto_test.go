package main

import (
	"encoding/json"
	"testing"
)

func TestDecodeClientFrameCall(t *testing.T) {
	f, err := decodeClientFrame([]byte(`{"t":"call","id":3,"cmd":"list_workspaces","args":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.T != "call" || f.ID != 3 || f.Cmd != "list_workspaces" {
		t.Fatalf("bad decode: %+v", f)
	}
}

func TestDecodeClientFrameKeepsArgsRaw(t *testing.T) {
	// Args stay json.RawMessage so callApp can decode each one straight into
	// its parameter's own type.
	f, err := decodeClientFrame([]byte(`{"t":"call","id":1,"cmd":"write_pty","args":{"id":7,"data":[3]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Args["id"]) != "7" {
		t.Fatalf("id arg not raw: %q", f.Args["id"])
	}
	if string(f.Args["data"]) != "[3]" {
		t.Fatalf("data arg not raw: %q", f.Args["data"])
	}
}

func TestDecodeClientFrameRejectsGarbage(t *testing.T) {
	if _, err := decodeClientFrame([]byte(`{not json`)); err == nil {
		t.Fatal("garbage must not decode")
	}
}

func TestDecodeClientFrameRejectsUnknownTag(t *testing.T) {
	if _, err := decodeClientFrame([]byte(`{"t":"telepathy"}`)); err == nil {
		t.Fatal("an unknown frame tag must be rejected, not ignored")
	}
}

func TestServerFramesOmitEmptyFields(t *testing.T) {
	// A reply must not carry an `error` key, and an event must not carry an
	// `id` — the client routes on their presence.
	b, err := json.Marshal(replyFrame(4, map[string]int{"n": 1}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["error"]; ok {
		t.Errorf("reply carries an error key: %s", b)
	}
	if m["t"] != "reply" || m["id"] != float64(4) {
		t.Errorf("bad reply shape: %s", b)
	}

	b, _ = json.Marshal(eventFrame("phase-pty:7", nil))
	m = map[string]any{}
	_ = json.Unmarshal(b, &m)
	if _, ok := m["id"]; ok {
		t.Errorf("event carries an id: %s", b)
	}
	if m["t"] != "event" || m["name"] != "phase-pty:7" {
		t.Errorf("bad event shape: %s", b)
	}
}

func TestErrorFrameCarriesCodeAndMessage(t *testing.T) {
	f := errorFrame(9, "unknown_command", `no such command "telepathy"`)
	if f.Error == nil || f.Error.Code != "unknown_command" {
		t.Fatalf("bad error frame: %+v", f)
	}
	if f.ID != 9 {
		t.Fatalf("error frame lost its id: %+v", f)
	}
}
