package main

import "testing"

func TestHookPortNeedsReclaim(t *testing.T) {
	alive := func(p int) bool { return p == 4000 }
	for _, c := range []struct {
		file string
		want bool
	}{
		{"5000", false},   // ours
		{"5000\n", false}, // ours, newline
		{"4000", false},   // another live instance: leave it, don't flap
		{"63214", true},   // another instance wrote it and quit (the bug)
		{"", true},        // deleted
		{"garbage", true}, // unreadable
	} {
		if got := hookPortNeedsReclaim(c.file, 5000, alive); got != c.want {
			t.Errorf("file %q: reclaim=%v, want %v", c.file, got, c.want)
		}
	}
}
