package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

// End-to-end versions of the remote-access scenarios in
// docs/plans/003-manual-test.md §11 — each drives the real /v2/ws handler over
// a real socket, so what they pin is what a phone actually sees.

// readUntil reads frames until match accepts one, failing after 2 s.
func readUntil(t *testing.T, conn interface {
	ReadJSON(any) error
	SetReadDeadline(time.Time) error
}, what string, match func(serverFrame) bool) serverFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var f serverFrame
		if err := conn.ReadJSON(&f); err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if match(f) {
			return f
		}
	}
}

// §11e — ESC typed on the phone mid-turn settles the phase, exactly as the
// desktop keystroke does: there is one WritePty and the socket reaches it.
func TestPhoneEscMidTurnInterruptsThePhase(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()
	ps, dir := newTestStore(t)
	// No daemon is listening, so the write itself fails — the phase is
	// applied before the write, which is what keeps a cancel from being lost
	// behind a daemon hiccup.
	app := &App{phases: ps, daemon: NewDaemonClient(context.Background(), filepath.Join(dir, "none.sock"))}
	conn, _ := dialTestWS(t, app)

	ps.Apply("pty:7", agentphase.Event{Kind: agentphase.HookRunning})

	write := func(id int64, data string) {
		t.Helper()
		f := clientFrame{T: "call", ID: id, Cmd: "write_pty", Args: map[string]json.RawMessage{
			"id": json.RawMessage(`"7"`), "data": json.RawMessage(data),
		}}
		if err := conn.WriteJSON(f); err != nil {
			t.Fatal(err)
		}
		readUntil(t, conn, "write_pty reply", func(f serverFrame) bool { return f.T == "reply" && f.ID == id })
	}

	// An arrow key starts with ESC too. Length is what separates it from a
	// cancel, so it must leave the turn running.
	write(1, `[27,91,65]`)
	if got := ps.Get("pty:7").State; got != agentphase.Running {
		t.Fatalf("arrow key changed the phase to %q", got)
	}

	write(2, `[27]`)
	if got := ps.Get("pty:7").State; got == agentphase.Running {
		t.Fatal("ESC from the phone left the turn running")
	}
}

// §11b — the socket dies mid-turn, the app keeps emitting, the phone
// reconnects on a NEW socket and resumes from the seq it last saw. It gets
// exactly the gap; the PTY bytes emitted meanwhile are gone (documented).
func TestReconnectAfterADroppedSocketResumesTheGap(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	a, _ := dialTestWS(t, &App{})
	busEmit("phase-pty:7", map[string]string{"state": "running"})
	seen := readUntil(t, a, "live phase", func(f serverFrame) bool { return f.T == "shell" })
	last := seen.Events[0].Seq
	a.Close()

	// While the phone is away.
	busEmit("pty-data-7", []int{104, 105})
	busEmit("phase-pty:7", map[string]string{"state": "done"})
	busEmit("workspaces-changed", nil)

	b, _ := dialTestWS(t, &App{})
	if err := b.WriteJSON(clientFrame{T: "resume", Since: last}); err != nil {
		t.Fatal(err)
	}
	f := readUntil(t, b, "resume deltas", func(f serverFrame) bool { return f.T == "shell" || f.T == "resync" })
	if f.T != "shell" {
		t.Fatalf("a gap inside the ring answered %q, want deltas", f.T)
	}
	var names []string
	for _, ev := range f.Events {
		names = append(names, ev.Name)
	}
	if strings.Join(names, ",") != "phase-pty:7,workspaces-changed" {
		t.Fatalf("resume delivered %v, want exactly the two shell events of the gap", names)
	}
}

// §11d — heavy terminal output to a client that stopped reading drops THAT
// client, never blocks busEmit, and leaves a healthy client streaming.
func TestQueueFullDropsOnlyTheSlowClient(t *testing.T) {
	t.Cleanup(busReset)
	busReset()

	slow, _ := dialTestWS(t, &App{})
	healthy, _ := dialTestWS(t, &App{})

	// Big frames wedge the slow writer in the kernel write (see
	// TestWSDropsClientWhenOutboundQueueFills). Each emit waits for the
	// healthy client to read it, so its own queue never holds more than one
	// frame and only the slow client's can fill.
	chunk := strings.Repeat("x", 512*1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < outboundQueue+16; i++ {
			busEmit("pty-data-7", chunk)
			_ = healthy.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := healthy.ReadMessage(); err != nil {
				t.Errorf("healthy client lost the stream at frame %d: %v", i, err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("busEmit blocked behind a client that stopped reading")
	}

	_ = slow.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := slow.ReadMessage(); err != nil {
			if isTimeout(err) {
				t.Fatalf("slow client was not dropped: %v", err)
			}
			break
		}
	}
}

// Concurrent emitters must reach every client in seq order. The client drops
// any event at or behind its high-water mark (transport.ts), so seq 6 landing
// before seq 5 would lose seq 5 for good.
func TestConcurrentEmitsArriveInSeqOrder(t *testing.T) {
	t.Cleanup(busReset)
	busReset()
	shellStreamReset()

	var mu sync.Mutex
	var got []int64
	busSubscribe(func(ev shellEvent) {
		mu.Lock()
		got = append(got, ev.Seq)
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				busEmit("workspaces-changed", nil)
			}
		}()
	}
	wg.Wait()

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("seq %d delivered after seq %d", got[i], got[i-1])
		}
	}
}
