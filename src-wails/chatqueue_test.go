package main

import (
	"errors"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

// Notices first, then the user's queue by position, then schedules; ties by
// creation — and the input is not mutated.
func TestDeliveryOrder(t *testing.T) {
	in := []pendingDelivery{
		{ChatID: 1, Source: deliverSchedule, Pos: 1},
		{ChatID: 1, Source: deliverQueue, Pos: 2},
		{ChatID: 1, Source: deliverQueue, Pos: -1}, // requeued head
		{ChatID: 1, Source: deliverNotice, Pos: 0, CreatedAt: 9},
		{ChatID: 1, Source: deliverQueue, Pos: 2, CreatedAt: -1},
	}
	got := deliveryOrder(in)
	want := []struct {
		src      deliverySource
		pos, cre int64
	}{{deliverNotice, 0, 9}, {deliverQueue, -1, 0}, {deliverQueue, 2, -1}, {deliverQueue, 2, 0}, {deliverSchedule, 1, 0}}
	for i, w := range want {
		if got[i].Source != w.src || got[i].Pos != w.pos || got[i].CreatedAt != w.cre {
			t.Fatalf("order[%d] = %+v, want %+v", i, got[i], w)
		}
	}
	if in[0].Source != deliverSchedule {
		t.Fatal("deliveryOrder mutated its input")
	}
}

type imgSink struct {
	to   []int64
	text []string
	err  error
}

func (s *imgSink) send(chatID int64, text string, _ []string) error {
	if s.err != nil {
		return s.err
	}
	s.to, s.text = append(s.to, chatID), append(s.text, text)
	return nil
}

// The queue drains head first, one item per settled chat per pass, never into
// a running turn; reorder / edit / remove are what it sends.
func TestChatQueueDrainsAfterSettle(t *testing.T) {
	a, ws := noticeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	a.phases.Apply(chatKey(c.ID), agentphase.Event{Kind: agentphase.HookRunning})

	x, _ := a.ChatQueueAdd(c.ID, "x", nil)
	y, _ := a.ChatQueueAdd(c.ID, "y", []string{"data:image/png;base64,AA=="})
	z, _ := a.ChatQueueAdd(c.ID, "z", nil)
	if err := a.ChatQueueReorder(c.ID, []int64{z.ID, x.ID, y.ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatQueueUpdate(x.ID, "x2"); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatQueueRemove(y.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatQueueRemove(y.ID); !errors.Is(err, errNotQueued) {
		t.Fatalf("second remove = %v, want errNotQueued", err)
	}

	s := &imgSink{}
	a.deliverPending(time.Now(), s.send)
	if len(s.text) != 0 {
		t.Fatalf("sent %v into a running turn", s.text)
	}
	a.phases.Apply(chatKey(c.ID), agentphase.Event{Kind: agentphase.HookDone})
	a.deliverPending(time.Now(), s.send)
	a.deliverPending(time.Now(), s.send)
	if len(s.text) != 2 || s.text[0] != "z" || s.text[1] != "x2" {
		t.Fatalf("sent %v, want [z x2]", s.text)
	}
	if q, _ := a.ChatQueueList(c.ID); len(q) != 0 {
		t.Fatalf("queue left %v", q)
	}
}

// A failed send puts the item back at the head under its old id (ahead of one
// queued meanwhile); after chatQueueMaxAttempts it stops draining until edited.
func TestChatQueueFailedSendRequeuesAtHead(t *testing.T) {
	a, ws := noticeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	first, _ := a.ChatQueueAdd(c.ID, "first", nil)
	a.ChatQueueAdd(c.ID, "second", nil)

	s := &imgSink{err: errors.New("agent won't start")}
	for i := 0; i < chatQueueMaxAttempts+2; i++ {
		a.deliverPending(time.Now(), s.send)
	}
	q, _ := a.ChatQueueList(c.ID)
	if len(q) != 2 || q[0].ID != first.ID || q[0].Attempts != chatQueueMaxAttempts || q[1].Text != "second" {
		t.Fatalf("queue = %+v", q)
	}

	// A busy chat (lost the race to the user's own send) is not an attempt.
	_ = a.ChatQueueUpdate(first.ID, "first")
	s.err = errChatBusy
	a.deliverPending(time.Now(), s.send)
	if q, _ := a.ChatQueueList(c.ID); q[0].Attempts != 0 {
		t.Fatalf("busy counted as an attempt: %+v", q[0])
	}

	s.err = nil
	a.deliverPending(time.Now(), s.send)
	if len(s.text) != 1 || s.text[0] != "first" {
		t.Fatalf("sent %v after edit", s.text)
	}
}

// One settled chat with a notice, a queued follow-up and a due schedule gets
// them in that order, one per pass; a deleted chat's queue goes with it.
func TestDeliveryNoticeThenQueueThenSchedule(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", Title: "kid", ParentChatID: parent.ID})
	assistantSays(t, a, child.ID, "done")

	scheduleAdapter{a}.addAt(parent.ID, "scheduled", time.Now().Add(-time.Minute))
	a.ChatQueueAdd(parent.ID, "queued", nil)
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTurnCompleted})

	s := &imgSink{}
	for i := 0; i < 3; i++ {
		a.deliverPending(time.Now(), s.send)
	}
	if len(s.text) != 3 || s.text[1] != "queued" || s.text[2] != "scheduled" {
		t.Fatalf("sent %q", s.text)
	}

	other, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	a.ChatQueueAdd(other.ID, "orphan", nil)
	if err := a.DeleteChat(other.ID); err != nil {
		t.Fatal(err)
	}
	if q, _ := a.ChatQueueList(other.ID); len(q) != 0 {
		t.Fatalf("deleted chat kept its queue: %v", q)
	}
}
