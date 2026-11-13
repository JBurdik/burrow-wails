package main

import "testing"

func TestChatPins(t *testing.T) {
	a := newTestApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	if _, err := a.AddPin(c.ID, "/tmp/mock.html", "check the layout", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddPin(c.ID, "/tmp/mock.html", "layout v2", true); err != nil {
		t.Fatal(err)
	}
	_, _ = a.AddPin(c.ID, "https://github.com/o/r/pull/42", "", false)
	pins, _ := a.ChatPinsList(c.ID)
	if len(pins) != 2 || pins[1].Note != "layout v2" {
		t.Fatalf("re-pinning must update, not duplicate: %+v", pins)
	}
	if err := a.ChatPinRemove(pins[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_pins`).Scan(&left)
	if left != 0 {
		t.Fatalf("DeleteChat left %d pins", left)
	}
}
