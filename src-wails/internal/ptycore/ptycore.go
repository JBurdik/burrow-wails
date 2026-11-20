// Package ptycore owns live PTY sessions. It has no Wails dependency so it
// can run both inside the app process and inside the standalone
// burrow-daemon binary (daemon survives app restart, matching the Rust
// backend's daemon_main.rs / daemon_client.rs split).
package ptycore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Events is how the manager reports PTY activity to its host (the app
// process re-emits these as Wails events; the daemon re-emits them as
// socket data frames to attached clients).
type Events interface {
	// off is the offset of data's first byte in everything this PTY has
	// written, so a client that lost some can ask Since for exactly that.
	OnData(id string, off uint64, data []byte)
	OnExit(id string)
	// OnAltExit: the PTY just left the alt screen (a TUI exited). A view that
	// attached while it was up has only the alt screen — the primary is
	// unreachable then (plan 010 Q2) — and re-attaches to get it. `vt` only.
	OnAltExit(id string)
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*session
	events   Events
}

type session struct {
	id   string
	cmd  *exec.Cmd
	file *os.File
	ring *byteRing
	// vtMu makes "ring end + emulator state" one atomic read: pump writes both
	// under it, so a snapshot's offset is exactly where its bytes stop. vt is
	// nil without the `vt` build tag (or if libghostty failed to start).
	vtMu sync.Mutex
	vt   *screen
	alt  bool // the emulator's alt screen was up after the last write
}

// ErrUnsupported: this daemon has no terminal emulator for the PTY (built
// without the `vt` tag). Callers fall back to the UI / cold ring replay.
var ErrUnsupported = errors.New("pty screen unsupported")

func NewManager(events Events) *Manager {
	m := &Manager{sessions: make(map[string]*session), events: events}
	go m.compressLoop()
	return m
}

// compressLoop compresses every emulator's cold scrollback pages (plan 010:
// ~8.8 MB resident → ~1.6 MB at the 8 MiB cap). Incremental, so a page already
// compressed costs nothing. ponytail: fixed 30 s for every PTY, busy or not.
func (m *Manager) compressLoop() {
	for range time.Tick(30 * time.Second) {
		m.mu.Lock()
		all := make([]*session, 0, len(m.sessions))
		for _, s := range m.sessions {
			all = append(all, s)
		}
		m.mu.Unlock()
		for _, s := range all {
			s.vtMu.Lock()
			if s.vt != nil {
				s.vt.compress()
			}
			s.vtMu.Unlock()
		}
	}
}

func errUnknown(id string) error { return fmt.Errorf("unknown pty id: %s", id) }

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/zsh"
}

// Create spawns the user's shell in cwd at the given size, with extra env,
// and starts streaming its output via Events.OnData. id is caller-supplied
// (the frontend owns its own pty-id counter, matching src-tauri's
// create_pty(id: u32, cwd, cols, rows, ...) — the backend never generates
// the id or picks the shell from JS args).
func (m *Manager) Create(id, cwd string, cols, rows uint16, env []string) error {
	// A live session under this id already exists — the caller is reattaching
	// (app restart, dev hot-reload, a reused id), not asking for a fresh shell.
	// Spawning unconditionally here would orphan the running one: pump() would
	// keep blocking on the old fd forever, m.sessions[id] would point only at
	// the new session, and KillPty could never reach the old process or pty
	// again. Reattach is a no-op — the existing pump goroutine is already
	// broadcasting this session's output to every daemon client, including a
	// freshly (re)connected one.
	if _, ok := m.get(id); ok {
		return nil
	}

	c := exec.Command(defaultShell(), "-l")
	if cwd != "" {
		c.Dir = cwd
	}
	// BURROW_PTY_ID lets the `burrow` CLI (and status hooks) inside this
	// session address itself.
	baseEnv := append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=Burrow",
	)
	c.Env = append(append(baseEnv, env...), "BURROW_PTY_ID="+id)

	f, err := pty.StartWithSize(c, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return err
	}

	sess := &session{id: id, cmd: c, file: f, ring: newByteRing(ringSize), vt: newScreen(cols, rows)}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.pump(sess)

	return nil
}

func (m *Manager) pump(sess *session) {
	buf := make([]byte, 32*1024)
	for {
		n, err := sess.file.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			sess.vtMu.Lock()
			off := sess.ring.write(data)
			altExit := false
			if sess.vt != nil {
				sess.vt.write(data)
				alt := sess.vt.alt()
				altExit, sess.alt = sess.alt && !alt, alt
			}
			sess.vtMu.Unlock()
			m.events.OnData(sess.id, off, data)
			if altExit {
				m.events.OnAltExit(sess.id) // after the data: the resync covers it
			}
		}
		if err != nil {
			break
		}
	}
	sess.vtMu.Lock()
	if sess.vt != nil {
		sess.vt.close()
		sess.vt = nil
	}
	sess.vtMu.Unlock()
	m.mu.Lock()
	delete(m.sessions, sess.id)
	m.mu.Unlock()
	m.events.OnExit(sess.id)
}

func (m *Manager) Write(id string, data []byte) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknown(id)
	}
	_, err := sess.file.Write(data)
	return err
}

func (m *Manager) Resize(id string, cols, rows uint16) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknown(id)
	}
	// The PTY's size IS the emulator's size (one size per PTY, no per-client
	// emulators): both change together.
	if err := pty.Setsize(sess.file, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return err
	}
	sess.vtMu.Lock()
	if sess.vt != nil {
		sess.vt.resize(cols, rows)
	}
	sess.vtMu.Unlock()
	return nil
}

// ScreenText is the tail of what the PTY's active screen shows, as plain text
// (soft wraps joined, rows right-trimmed, trailing blank rows dropped) — what
// tab_output returns, readable with no window open.
func (m *Manager) ScreenText(id string, lines int) (string, error) {
	sess, ok := m.get(id)
	if !ok {
		return "", errUnknown(id)
	}
	sess.vtMu.Lock()
	defer sess.vtMu.Unlock()
	if sess.vt == nil {
		return "", ErrUnsupported
	}
	return sess.vt.text(lines)
}

// ScreenFlags: the alt screen is up / the cursor is at a marked shell prompt,
// from the emulator. Both false without one (or for an unknown id) — the
// foreground poll's tie-breakers, never stronger than the process name.
func (m *Manager) ScreenFlags(id string) (alt, atPrompt bool) {
	sess, ok := m.get(id)
	if !ok {
		return false, false
	}
	sess.vtMu.Lock()
	defer sess.vtMu.Unlock()
	if sess.vt == nil {
		return false, false
	}
	return sess.vt.alt(), sess.vt.atPrompt()
}

// Snapshot rebuilds a PTY's screen in a fresh xterm: VT bytes for the active
// screen + history + modes, cut at ring offset Off — every pty-data chunk at or
// past Off follows it with no gap and no overlap.
type Snapshot struct {
	Off        uint64
	VT         []byte
	Cols, Rows uint16
	Alt        bool
}

func (m *Manager) Attach(id string) (Snapshot, error) {
	sess, ok := m.get(id)
	if !ok {
		return Snapshot{}, errUnknown(id)
	}
	sess.vtMu.Lock()
	defer sess.vtMu.Unlock()
	if sess.vt == nil {
		return Snapshot{}, ErrUnsupported
	}
	vt, cols, rows, alt, err := sess.vt.snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	// ring.end without ring.mu: its only writer (pump) also holds vtMu.
	return Snapshot{Off: sess.ring.end, VT: vt, Cols: cols, Rows: rows, Alt: alt}, nil
}

func (m *Manager) Kill(id string) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknown(id)
	}
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
	return sess.file.Close()
}

// Since returns this PTY's output from offset `from` on, and the offset it
// actually starts at — later than `from` when the ring no longer holds it all.
func (m *Manager) Since(id string, from uint64) (uint64, []byte, error) {
	sess, ok := m.get(id)
	if !ok {
		return 0, nil, errUnknown(id)
	}
	off, data := sess.ring.since(from)
	return off, data, nil
}

func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) get(id string) (*session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Foreground returns the command name of the process group currently in the
// foreground of this PTY — the answer to "what is the user actually running in
// that tab". Empty when the pty is unknown, already dead, or has no foreground
// group at all.
//
// This is the terminal's own notion of foreground (TIOCGPGRP on the master fd,
// i.e. tcgetpgrp), not a guess from the process tree: it is exactly what the
// kernel would deliver a Ctrl-C to, so it flips the moment a command starts or
// exits. The shell IS reported by name when it is in the foreground — that is
// how the caller learns a command or agent has exited and it is back at the
// prompt (XTerm.vue's SHELL_RE branch).
//
// ponytail: `p_comm` from sysctl rather than shelling out to `ps`. The poll
// runs every 2 s per terminal, and the name is all any caller matches on
// (`/^claude$/`, `/^zsh$/`). p_comm is truncated to 16 chars by the kernel,
// which no command name we match comes close to.
func (m *Manager) Foreground(id string) (string, error) {
	name, _, err := m.ForegroundInfo(id)
	return name, err
}

// ForegroundInfo is Foreground plus the foreground process group id. A job-
// controlling shell makes the command it launches the leader of its own group,
// so for `claude` typed at a prompt the pgid IS claude's pid — the key of its
// ~/.claude/sessions/<pid>.json. 0 when there is no foreground group.
func (m *Manager) ForegroundInfo(id string) (string, int, error) {
	sess, ok := m.get(id)
	if !ok {
		return "", 0, errUnknown(id)
	}
	pgid, err := unix.IoctlGetInt(int(sess.file.Fd()), unix.TIOCGPGRP)
	if err != nil || pgid <= 0 {
		// No foreground group: the session is being torn down. Not an error the
		// caller can act on — a poll that reports nothing is the truthful answer.
		return "", 0, nil
	}
	return processName(pgid), pgid, nil
}

func processName(pid int) string {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return "" // exited between the ioctl and the lookup
	}
	comm := kp.Proc.P_comm
	buf := make([]byte, 0, len(comm))
	for _, c := range comm {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}
