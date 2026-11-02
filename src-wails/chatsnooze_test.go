package main

import (
	"testing"
	"time"
)

func TestChatSnoozeAndWake(t *testing.T) {
	a := newTestApp(t)
	c, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE chats SET settled_override = 'settled' WHERE id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000)
	until := now.Add(time.Hour).UnixMilli()
	if err := a.ChatSnooze(c.ID, until); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatSnooze(9999, until); err == nil {
		t.Fatal("snoozing a missing chat must fail")
	}
	got, _ := a.chatByID(c.ID)
	if got.SnoozedUntil != until {
		t.Fatalf("snoozed_until = %d, want %d", got.SnoozedUntil, until)
	}

	// SaveChats must not touch it (read-only on the wire).
	got.SnoozedUntil = 0
	if err := a.SaveChats([]Chat{got}); err != nil {
		t.Fatal(err)
	}
	if got, _ = a.chatByID(c.ID); got.SnoozedUntil != until {
		t.Fatalf("SaveChats wrote snoozed_until: %d", got.SnoozedUntil)
	}

	if woke := a.wakeSnoozedChats(now); len(woke) != 0 {
		t.Fatalf("woke early: %v", woke)
	}
	seq := currentSeq()
	later := now.Add(2 * time.Hour)
	if woke := a.wakeSnoozedChats(later); len(woke) != 1 || woke[0] != c.ID {
		t.Fatalf("woke = %v, want [%d]", woke, c.ID)
	}
	got, _ = a.chatByID(c.ID)
	if got.SnoozedUntil != 0 || got.SettledOverride != "" || got.LastActivityAt != later.UnixMilli() {
		t.Fatalf("after wake: %+v", got)
	}
	evs, _ := resumeSince(seq)
	names := map[string]bool{}
	for _, ev := range evs {
		names[ev.Name] = true
	}
	if !names["chats-changed"] || !names["chat-snooze-woke"] {
		t.Fatalf("events = %v", names)
	}
	if woke := a.wakeSnoozedChats(later); len(woke) != 0 {
		t.Fatalf("woke twice: %v", woke)
	}

	// Unsnooze clears without waking.
	_ = a.ChatSnooze(c.ID, later.Add(time.Hour).UnixMilli())
	_ = a.ChatSnooze(c.ID, 0)
	if got, _ = a.chatByID(c.ID); got.SnoozedUntil != 0 {
		t.Fatalf("unsnooze: %d", got.SnoozedUntil)
	}
}
