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
