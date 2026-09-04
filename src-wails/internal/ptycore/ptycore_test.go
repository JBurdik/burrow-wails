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
