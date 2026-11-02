package ptycore

import (
	"bytes"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"
)

type nopEvents struct{}

func (nopEvents) OnData(string, uint64, []byte) {}
func (nopEvents) OnExit(string)                 {}

var shellRE = regexp.MustCompile(`^(zsh|bash|sh|fish|csh|tcsh|dash)$`)

// Foreground decides a status dot for everything the agent hooks cannot see, so
// it has to name a REAL process — an empty answer is indistinguishable from
// "nothing is running" to the caller.
func TestForegroundNamesTheShell(t *testing.T) {
	m := NewManager(nopEvents{})
	if err := m.Create("t1", t.TempDir(), 80, 24, nil); err != nil {
		// Some sandboxes cannot allocate a pty at all (ENXIO). That is the
		// environment failing, not this code — skip rather than cry wolf.
		t.Skipf("cannot allocate a pty here: %v", err)
	}
	defer m.Kill("t1")

	// The shell has to reach the foreground first; a login shell takes a moment.
	var name string
	for i := 0; i < 100; i++ {
		got, err := m.Foreground("t1")
		if err != nil {
			t.Fatalf("foreground: %v", err)
		}
		if got != "" {
			name = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !shellRE.MatchString(name) {
		t.Fatalf("want a shell name the frontend's SHELL_RE matches, got %q", name)
	}
}

func TestForegroundUnknownPtyIsAnError(t *testing.T) {
	m := NewManager(nopEvents{})
	// Distinct from "": the caller reads an empty name as "nothing running",
	// which a pty that does not exist at all is not.
	if _, err := m.Foreground("nope"); err == nil {
		t.Fatal("want an error for an unknown pty id")
	}
}

// A reattach (app restart, dev hot-reload, a reused id) calls Create again for
// an id that already has a live session. It must be a no-op: spawning a second
// shell under the same id would leak the first one — orphaned in no map entry,
// unreachable by Kill, its pty device never freed.
func TestCreateOnLiveIdDoesNotLeakTheOldSession(t *testing.T) {
	m := NewManager(nopEvents{})
	if err := m.Create("t1", t.TempDir(), 80, 24, nil); err != nil {
		t.Skipf("cannot allocate a pty here: %v", err)
	}
	defer m.Kill("t1")

	first, _ := m.get("t1")
	firstPID := first.cmd.Process.Pid

	if err := m.Create("t1", t.TempDir(), 80, 24, nil); err != nil {
		t.Fatalf("reattach Create: %v", err)
	}

	second, ok := m.get("t1")
	if !ok {
		t.Fatal("session vanished after reattach Create")
	}
	if second.cmd.Process.Pid != firstPID {
		t.Fatalf("reattach spawned a new shell (pid %d -> %d), leaking the old one", firstPID, second.cmd.Process.Pid)
	}
}

type recEvents struct {
	mu  sync.Mutex
	out []byte
	bad string
}

func (r *recEvents) OnData(_ string, off uint64, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if off != uint64(len(r.out)) && r.bad == "" {
		r.bad = fmt.Sprintf("chunk at off %d, want %d", off, len(r.out))
	}
	r.out = append(r.out, data...)
}
func (*recEvents) OnExit(string) {}

// Every chunk's offset continues where the previous one ended, and Since hands
// back exactly the bytes that were streamed — the contract a reconnecting
// terminal view resumes on.
func TestOffsetsAreContiguousAndSinceMatchesStream(t *testing.T) {
	ev := &recEvents{}
	m := NewManager(ev)
	if err := m.Create("t1", t.TempDir(), 80, 24, nil); err != nil {
		t.Skipf("cannot allocate a pty here: %v", err)
	}
	defer m.Kill("t1")
	_ = m.Write("t1", []byte("echo burrow-since-marker\n"))

	for i := 0; i < 200; i++ {
		ev.mu.Lock()
		done := bytes.Count(ev.out, []byte("burrow-since-marker")) >= 2
		ev.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.bad != "" {
		t.Fatal(ev.bad)
	}
	if len(ev.out) < 4 {
		t.Fatalf("too little output to test with: %q", ev.out)
	}
	off, data, err := m.Since("t1", 3)
	if err != nil {
		t.Fatal(err)
	}
	// The shell may have written more since the lock above; compare the prefix.
	if off != 3 || !bytes.HasPrefix(data, ev.out[3:]) {
		t.Fatalf("Since(3) = %d %q, streamed %q", off, data, ev.out)
	}
	if _, _, err := m.Since("nope", 0); err == nil {
		t.Fatal("want an error for an unknown pty id")
	}
}
