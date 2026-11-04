package main

import "testing"

func TestPtySizeRemembersLastGrid(t *testing.T) {
	a := &App{}
	if g := a.PtySize("t-size"); g != (PtyGrid{}) {
		t.Fatalf("unknown pty: got %+v, want zero", g)
	}
	notePtySize("t-size", 120, 40)
	notePtySize("t-size", 132, 50)
	if g := a.PtySize("t-size"); g != (PtyGrid{132, 50}) {
		t.Fatalf("got %+v, want {132 50}", g)
	}
}
