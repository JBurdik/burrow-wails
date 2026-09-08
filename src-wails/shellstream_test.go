package main

import "testing"

func TestSeqIsMonotonic(t *testing.T) {
	shellStreamReset()
	a := recordShellEvent("workspaces-changed", nil)
	b := recordShellEvent("workspaces-changed", nil)
	if a.Seq <= 0 {
		t.Fatalf("first seq must be positive, got %d", a.Seq)
	}
	if b.Seq != a.Seq+1 {
		t.Fatalf("seq not consecutive: %d then %d", a.Seq, b.Seq)
	}
	if currentSeq() != b.Seq {
		t.Fatalf("currentSeq %d, want %d", currentSeq(), b.Seq)
	}
}

func TestResumeReturnsOnlyWhatCameAfter(t *testing.T) {
	shellStreamReset()
	first := recordShellEvent("a", nil)
	recordShellEvent("b", nil)
	recordShellEvent("c", nil)

	evs, ok := resumeSince(first.Seq)
	if !ok {
		t.Fatal("resume from a seq still in the ring must succeed")
	}
	if len(evs) != 2 {
		t.Fatalf("want the 2 events after %d, got %d", first.Seq, len(evs))
	}
	if evs[0].Name != "b" || evs[1].Name != "c" {
		t.Fatalf("wrong events or order: %+v", evs)
	}
}

func TestResumeFromCurrentSeqIsEmptyNotAResync(t *testing.T) {
	shellStreamReset()
	last := recordShellEvent("a", nil)

	evs, ok := resumeSince(last.Seq)
	if !ok {
		t.Fatal("a client that missed nothing must not be told to resync")
	}
	if len(evs) != 0 {
		t.Fatalf("want no events, got %+v", evs)
	}
}

func TestResumeBeyondTheRingFails(t *testing.T) {
	shellStreamReset()
	old := recordShellEvent("first", nil)
	for i := 0; i < shellRingSize+10; i++ {
		recordShellEvent("filler", nil)
	}

	if _, ok := resumeSince(old.Seq); ok {
		t.Fatal("a seq the ring has overwritten must report a gap")
	}
}

func TestResumeAfterProcessRestartFails(t *testing.T) {
	// A fresh ring is what a restarted process looks like: seq starts over, so
	// any client seq is from a state that no longer exists.
	shellStreamReset()
	if _, ok := resumeSince(42); ok {
		t.Fatal("a seq from before a restart must report a gap")
	}
}

func TestResumeFromZeroIsAGap(t *testing.T) {
	shellStreamReset()
	recordShellEvent("a", nil)
	// since 0 means "I have nothing", which is a snapshot, not a delta.
	if _, ok := resumeSince(0); ok {
		t.Fatal("since=0 must send the client to a snapshot")
	}
}

// TestResumeAtExactlyTheEvictedBoundary exercises resumeSince's tightest
// comparison directly, rather than the far-over-the-ring case
// TestResumeBeyondTheRingFails already covers: `since == oldest-1` is the
// exact boundary between "recoverable" and "gap", and it is the one place an
// off-by-one (`since < oldest-1` accidentally becoming `since <= oldest-1`,
// or the eviction trim shifting by one) would silently drop one event on
// every reconnect — the precise failure the ring exists to prevent.
//
// Semantics decided here: `since == oldest-1` means the client's last-seen
// event is exactly the one that just fell off the front of the ring. That
// event itself is gone, but the ring is a contiguous run ending at
// currentSeq, so everything the client is missing (oldest..currentSeq) is
// still present — nothing was actually lost. This must succeed and return
// the whole ring. Only `since < oldest-1` — the client is missing an event
// that was evicted *and* is unrecoverable — is a genuine gap.
func TestResumeAtExactlyTheEvictedBoundary(t *testing.T) {
	shellStreamReset()

	// Two throwaway events first so `old` below isn't seq 1 — otherwise
	// testing "one further back" would land on since=0, which is a
	// different special case (TestResumeFromZeroIsAGap), not this boundary.
	recordShellEvent("junk1", nil)
	recordShellEvent("junk2", nil)

	old := recordShellEvent("old", nil)

	// Exactly shellRingSize more events makes `old` the one event evicted:
	// the ring now holds old.Seq+1 .. currentSeq, i.e. oldest == old.Seq+1.
	for i := 0; i < shellRingSize; i++ {
		recordShellEvent("filler", nil)
	}

	// since == oldest-1: recoverable, must return the entire ring.
	evs, ok := resumeSince(old.Seq)
	if !ok {
		t.Fatal("since == oldest-1 must be recoverable: nothing after it was lost")
	}
	if len(evs) != shellRingSize {
		t.Fatalf("want the whole ring (%d events), got %d", shellRingSize, len(evs))
	}

	// since == oldest-2: the event just before `old` was itself evicted and
	// is unrecoverable, so this is a real gap.
	if _, ok := resumeSince(old.Seq - 1); ok {
		t.Fatal("since == oldest-2 must report a gap: an unrecoverable event was missed")
	}
}
