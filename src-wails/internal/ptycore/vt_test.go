//go:build vt

package ptycore

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// chunkEvents keeps every chunk with its offset, so a test can check that a
// snapshot's Off is exactly where the live stream continues.
type chunkEvents struct {
	mu       sync.Mutex
	data     map[uint64][]byte
	altExits int
}

func (r *chunkEvents) OnData(_ string, off uint64, d []byte) {
	r.mu.Lock()
	r.data[off] = d
	r.mu.Unlock()
}
func (r *chunkEvents) OnExit(string) {}
func (r *chunkEvents) OnAltExit(string) {
	r.mu.Lock()
	r.altExits++
	r.mu.Unlock()
}

func startShell(t *testing.T, ev Events) *Manager {
	t.Helper()
	m := NewManager(ev)
	if err := m.Create("v", t.TempDir(), 80, 24, []string{"PS1=$ ", "PROMPT=$ "}); err != nil {
		t.Skipf("cannot allocate a pty here: %v", err)
	}
	t.Cleanup(func() { m.Kill("v") })
	return m
}

func waitText(t *testing.T, m *Manager, want string) string {
	t.Helper()
	var got string
	for i := 0; i < 200; i++ {
		got, _ = m.ScreenText("v", 0)
		if strings.Contains(got, want) {
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q; last:\n%s", want, got)
	return ""
}

// tab_output's whole point: the daemon reads the screen with no UI attached,
// as clean text — no escape codes, soft wraps joined.
func TestScreenTextIsCleanAndUnwrapped(t *testing.T) {
	m := startShell(t, nopEvents{})
	long := strings.Repeat("x", 150) // wraps at 80 cols
	// Quotes split every marker, so the echoed command line never matches.
	m.Write("v", []byte("printf '\\033[1;31mR''ED\\033[0m done\\n'; printf 'x%.0s' $(seq 150); echo\r"))
	got := waitText(t, m, long)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("escape codes leaked into text:\n%q", got)
	}
	if !strings.Contains(got, "RED done") {
		t.Fatalf("want styled text as plain, got:\n%s", got)
	}
	tail, _ := m.ScreenText("v", 2)
	if n := strings.Count(tail, "\n"); n != 1 {
		t.Fatalf("lines=2 should give 2 rows, got %d:\n%s", n+1, tail)
	}
}

// Resize reflows the emulator with the PTY: no per-client sizes.
func TestResizeFollowsThePty(t *testing.T) {
	m := startShell(t, nopEvents{})
	if err := m.Resize("v", 132, 40); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Attach("v")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cols != 132 || snap.Rows != 40 {
		t.Fatalf("snapshot size %dx%d, want 132x40", snap.Cols, snap.Rows)
	}
}

// A TUI on the alt screen: the snapshot says so, and leaving restores the
// primary screen's text (plan 010 Phase 0, Q2).
func TestAltScreenIsReported(t *testing.T) {
	ev := &chunkEvents{data: map[uint64][]byte{}}
	m := startShell(t, ev)
	m.Write("v", []byte("echo PRI''MARY; printf '\\033[?1049hALT''SCREEN'; sleep 1; printf '\\033[?1049l'\r"))
	waitText(t, m, "ALTSCREEN")
	snap, err := m.Attach("v")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Alt {
		t.Fatal("want Alt while the alt screen is up")
	}
	if alt, _ := m.ScreenFlags("v"); !alt {
		t.Fatal("ScreenFlags must report the alt screen to the foreground poll")
	}
	got := waitText(t, m, "PRIMARY")
	if strings.Contains(got, "ALTSCREEN") {
		t.Fatalf("alt content after leaving:\n%s", got)
	}
	// Leaving fires exactly one resync: a view attached during the TUI has no
	// primary screen and must re-attach for it.
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.altExits != 1 {
		t.Fatalf("OnAltExit fired %d times, want 1", ev.altExits)
	}
}

// The snapshot is cut at ring offset Off: the live stream past Off is exactly
// what the client applies next. Run under -race with output still flowing.
func TestAttachOffsetMatchesTheStream(t *testing.T) {
	ev := &chunkEvents{data: map[uint64][]byte{}}
	m := startShell(t, ev)
	m.Write("v", []byte("for i in $(seq 1 3000); do echo line$i; done\r"))
	time.Sleep(50 * time.Millisecond) // mid-flood
	snap, err := m.Attach("v")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.VT) == 0 || snap.Off == 0 {
		t.Fatalf("empty snapshot: off=%d vt=%d", snap.Off, len(snap.VT))
	}
	waitText(t, m, "line3000")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if _, ok := ev.data[snap.Off]; !ok {
		// Off must be a chunk boundary: the snapshot never splits a chunk.
		t.Fatalf("Off %d is not a chunk boundary of the live stream", snap.Off)
	}
	if !bytes.Contains(snap.VT, []byte("line")) {
		t.Fatal("snapshot does not contain the flood")
	}
}

func TestUnknownPtyScreenIsAnError(t *testing.T) {
	m := NewManager(nopEvents{})
	if _, err := m.ScreenText("nope", 10); err == nil || err == ErrUnsupported {
		t.Fatalf("want unknown-pty error, got %v", err)
	}
}

// OSC 133 (A = prompt start, B = input start): the cursor on a marked prompt is
// how the poll recognises a shell shellRE does not know.
func TestScreenFlagsSeeAMarkedPrompt(t *testing.T) {
	m := startShell(t, nopEvents{})
	m.Write("v", []byte("printf 'MARK''ED\\n\\033]133;A\\007nu> \\033]133;B\\007'; sleep 2\r"))
	waitText(t, m, "MARKED")
	time.Sleep(100 * time.Millisecond)
	if _, atPrompt := m.ScreenFlags("v"); !atPrompt {
		t.Fatal("cursor after OSC 133;B is at a prompt")
	}
}
