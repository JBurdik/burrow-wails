package main

import "testing"

func TestMergeWindowState(t *testing.T) {
	prev := windowState{X: 100, Y: 50, Width: 1200, Height: 800}
	full := windowState{X: 0, Y: 0, Width: 3440, Height: 1440, Maximised: true}

	// Maximised: keep the previous normal geometry, but record the flag.
	got := mergeWindowState(prev, full, true)
	if got.X != 100 || got.Y != 50 || got.Width != 1200 || got.Height != 800 || !got.Maximised {
		t.Fatalf("maximised merge lost normal geometry: %+v", got)
	}
	// Normal: current geometry wins.
	got = mergeWindowState(prev, windowState{X: 7, Y: 8, Width: 900, Height: 600}, false)
	if got.X != 7 || got.Width != 900 {
		t.Fatalf("normal merge should take current: %+v", got)
	}
	// No usable previous state (first launch, maximised): don't write junk.
	got = mergeWindowState(windowState{}, full, true)
	if got.Width != 3440 {
		t.Fatalf("empty prev should fall through: %+v", got)
	}
}
