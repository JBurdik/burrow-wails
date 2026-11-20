package main

import (
	"testing"
)

func userEvents(evs []ProviderRuntimeEvent) []ProviderRuntimeEvent {
	var out []ProviderRuntimeEvent
	for _, ev := range evs {
		if ev.Type == EvtUserDelta {
			out = append(out, ev)
		}
	}
	return out
}

// Settled → sent now, the user line carries the client id; in flight → queued.
func TestChatSubmitSendsWhenSettledQueuesWhenRunning(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	events := chatEvents(t, "1")

	res, err := a.ChatSubmit(c.ID, "first", nil, "c-1")
	if err != nil || !res.Sent || res.Item != nil {
		t.Fatalf("settled submit = %+v, %v; want sent", res, err)
	}
	if n := countSent(fake.sent, "first"); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	if ue := userEvents(events()); len(ue) != 1 || ue[0].ClientID != "c-1" || ue[0].Text != "first" {
		t.Fatalf("user line = %+v, want text+clientId", ue)
	}

	// The send put the chat in flight: the next submit queues.
	res, err = a.ChatSubmit(c.ID, "second", nil, "c-2")
	if err != nil || res.Sent || res.Item == nil || res.Item.ClientID != "c-2" {
		t.Fatalf("running submit = %+v, %v; want queued with clientId", res, err)
	}
	if n := countSent(fake.sent, "second"); n != 0 {
		t.Fatal("a running chat must not get the submit now")
	}

	// Repeats: the original outcome, no second send / insert.
	if r, _ := a.ChatSubmit(c.ID, "first", nil, "c-1"); !r.Sent {
		t.Fatalf("repeat of a sent id = %+v", r)
	}
	if r, _ := a.ChatSubmit(c.ID, "second", nil, "c-2"); r.Sent || r.Item == nil || r.Item.ID != res.Item.ID {
		t.Fatalf("repeat of a queued id = %+v", r)
	}
	if items, _ := a.ChatQueueList(c.ID); len(items) != 1 {
		t.Fatalf("queue = %d items, want 1", len(items))
	}
	if n := countSent(fake.sent, "first"); n != 1 {
		t.Fatalf("repeat re-sent: %d", n)
	}
}

// A core restart empties the in-memory ids; the queue row, the chat-user line
// and the transcript row still dedupe.
func TestChatSubmitDedupeSurvivesRestart(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})

	if _, err := a.ChatSubmit(c.ID, "hello", nil, "r-1"); err != nil {
		t.Fatal(err)
	}
	queued, err := a.ChatSubmit(c.ID, "later", nil, "r-2")
	if err != nil || queued.Item == nil {
		t.Fatalf("want queued: %+v %v", queued, err)
	}
	drainChatStream(t, a, "1")
	a.clientIDs = clientIDs{} // restart

	if r, err := a.ChatSubmit(c.ID, "hello", nil, "r-1"); err != nil || !r.Sent {
		t.Fatalf("restart repeat via chat-user line = %+v, %v", r, err)
	}
	if r, _ := a.ChatSubmit(c.ID, "later", nil, "r-2"); r.Item == nil || r.Item.ID != queued.Item.ID {
		t.Fatalf("restart repeat via queue row = %+v", r)
	}
	if n := countSent(fake.sent, "hello"); n != 1 {
		t.Fatalf("restart repeat re-sent: %d", n)
	}

	// Stream trimmed after the fold: the transcript row alone still counts.
	a.clientIDs = clientIDs{}
	if _, err := a.db.Exec(`DELETE FROM chat_stream WHERE chat_id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT OR REPLACE INTO chat_messages (chat_id, ord, payload_json) VALUES (?, 999, ?)`,
		c.ID, `{"id":999,"role":"user","text":"old","clientId":"r-3"}`); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSend(c.ID, "old", nil, "r-3"); err != nil {
		t.Fatal(err)
	}
	if n := countSent(fake.sent, "old"); n != 0 {
		t.Fatalf("restart repeat via transcript row re-sent: %d", n)
	}
}

func TestChatSteerDedupesClientID(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	events := chatEvents(t, "1")
	if err := a.ChatSend(c.ID, "go", nil, ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := a.ChatSteer(c.ID, "steer me", nil, "s-1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := countSent(fake.sent, "steer me"); n != 1 {
		t.Fatalf("steer delivered %d times", n)
	}
	ue := userEvents(events())
	if len(ue) != 2 || ue[1].ClientID != "s-1" {
		t.Fatalf("steer line = %+v, want clientId s-1", ue)
	}
}

// Old chat-user lines are the bare prompt; a prompt that merely looks like
// JSON stays the prompt.
func TestUserLineReadsOldAndNew(t *testing.T) {
	for _, tc := range []struct{ line, text, cid string }{
		{"hello", "hello", ""},
		{`{"a":1}`, `{"a":1}`, ""},
		{encodeUserLine("hi", []string{"data:x"}, "k"), "hi", "k"},
	} {
		evs := NormalizeChatLine(chatUserKind, tc.line, 7)
		if len(evs) != 1 || evs[0].Text != tc.text || evs[0].ClientID != tc.cid {
			t.Fatalf("%q → %+v", tc.line, evs)
		}
	}
	msgs := foldEvents(append(NormalizeChatLine(chatUserKind, "plain", 1),
		NormalizeChatLine(chatUserKind, encodeUserLine("new", []string{"data:img"}, "k2"), 2)...))
	if len(msgs) != 2 || msgs[0].Text != "plain" || msgs[0].ClientID != "" ||
		msgs[1].Text != "new" || msgs[1].ClientID != "k2" || len(msgs[1].Images) != 1 {
		t.Fatalf("fold = %+v", msgs)
	}
}
