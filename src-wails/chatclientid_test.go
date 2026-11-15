package main

import (
	"strings"
	"testing"
)

func countSent(sent []string, text string) int {
	n := 0
	for _, s := range sent {
		if strings.Contains(s, `"`+text+`"`) {
			n++
		}
	}
	return n
}

// A retried clientId is accepted once, whichever door it comes back through.
func TestClientIDDedupesSendAndQueue(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	c, err := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if err != nil {
		t.Fatal(err)
	}

	// double chat_send
	for range 2 {
		if err := a.ChatSend(c.ID, "once", nil, "id-1"); err != nil {
			t.Fatal(err)
		}
	}
	if n := countSent(fake.sent, "once"); n != 1 {
		t.Fatalf("double chat_send delivered %d times", n)
	}

	// double queue_add returns the first item
	q1, err := a.ChatQueueAdd(c.ID, "queued", nil, "id-2")
	if err != nil {
		t.Fatal(err)
	}
	q2, err := a.ChatQueueAdd(c.ID, "queued", nil, "id-2")
	if err != nil || q2.ID != q1.ID {
		t.Fatalf("double queue_add: %v, ids %d vs %d", err, q1.ID, q2.ID)
	}

	// send then queue with the same id: no insert
	if err := a.ChatSend(c.ID, "sent", nil, "id-3"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ChatQueueAdd(c.ID, "sent", nil, "id-3"); err != nil {
		t.Fatal(err)
	}
	items, _ := a.ChatQueueList(c.ID)
	if len(items) != 1 || items[0].ID != q1.ID {
		t.Fatalf("queue = %+v, want only the first queued item", items)
	}

	// empty id never dedupes; same id on another chat is independent
	other, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if _, err := a.ChatQueueAdd(other.ID, "x", nil, "id-2"); err != nil {
		t.Fatal(err)
	}
	a.ChatQueueAdd(other.ID, "x", nil, "")
	a.ChatQueueAdd(other.ID, "x", nil, "")
	if items, _ := a.ChatQueueList(other.ID); len(items) != 3 {
		t.Fatalf("other chat queue = %d items, want 3", len(items))
	}
}

// A failed attempt does not burn the id.
func TestClientIDFailedSendIsRetryable(t *testing.T) {
	var r clientIDs
	_, finish, dup := r.claim(1, "x")
	if dup {
		t.Fatal("fresh id reported dup")
	}
	finish(false, nil)
	_, finish, dup = r.claim(1, "x")
	if dup {
		t.Fatal("failed id must be claimable again")
	}
	finish(true, nil)
	if _, _, dup = r.claim(1, "x"); !dup {
		t.Fatal("accepted id must dedupe")
	}
}
