package main

import (
	"reflect"
	"testing"
	"time"
)

func TestQuitHold(t *testing.T) {
	var got []string
	h := &quitHold{
		emit:  func(s string) { got = append(got, s) },
		quit:  func() { got = append(got, "QUIT") },
		after: func(time.Duration, func()) {},
	}
	t0 := time.Unix(1000, 0)
	ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }

	// A tap: one press, then silence → cancelled, no quit.
	h.press(ms(0))
	h.settle(ms(300)) // stale timer, run still open
	h.settle(ms(700))
	// A hold: initial repeat delay, then repeats past 1 s → quits only once they stop.
	for _, n := range []int{2000, 2400, 2500, 2600, 2700, 2800, 2900, 3000, 3100} {
		h.press(ms(n))
	}
	h.settle(ms(3500)) // still within the gap of the last repeat
	h.settle(ms(3800))
	want := []string{"down", "up", "down", "quitting", "QUIT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
