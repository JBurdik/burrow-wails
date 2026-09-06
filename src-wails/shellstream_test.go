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
