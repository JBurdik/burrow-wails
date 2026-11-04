package main

import "testing"

func TestSeenReceiptsMoveForwardAndClear(t *testing.T) {
	a := newTestApp(t)
	first, err := a.SeenMark("chat:7")
	if err != nil || first == 0 {
		t.Fatalf("mark: %v %d", err, first)
	}
	// A stale higher value must survive a later mark with an older clock.
	if _, err := a.db.Exec(`UPDATE seen_receipts SET seen_at = ? WHERE key = 'chat:7'`, first+1_000_000); err != nil {
		t.Fatal(err)
	}
	if at, _ := a.SeenMark("chat:7"); at != first+1_000_000 {
		t.Fatalf("receipt moved backwards: %d", at)
	}
	a.seenClear("chat:7")
	if list, _ := a.SeenList(); len(list) != 0 {
		t.Fatalf("clear left %v", list)
	}
}
