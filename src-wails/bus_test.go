package main

import "testing"

func TestBusFansOutToEverySink(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	var a, b []string
	busSubscribe(func(name string, _ any) { a = append(a, name) })
	busSubscribe(func(name string, _ any) { b = append(b, name) })

	busEmit("workspaces-changed", nil)

	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("fanout failed: %v / %v", a, b)
	}
}

func TestBusWithNoSinksDoesNotPanic(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	busEmit("pty-data-1", []byte("hi"))
}

func TestBusUnsubscribeRemovesTheSink(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	var got []string
	unsub := busSubscribe(func(name string, _ any) { got = append(got, name) })
	busEmit("a", nil)
	unsub()
	busEmit("b", nil)

	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("unsubscribe did not take effect: %v", got)
	}
}
