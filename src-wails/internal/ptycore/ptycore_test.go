package ptycore

import (
	"regexp"
	"testing"
	"time"
)

type nopEvents struct{}

func (nopEvents) OnData(string, []byte) {}
func (nopEvents) OnExit(string)         {}

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
