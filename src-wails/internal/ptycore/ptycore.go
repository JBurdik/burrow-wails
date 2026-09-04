// Package ptycore owns live PTY sessions. It has no Wails dependency so it
// can run both inside the app process and inside the standalone
// burrow-daemon binary (daemon survives app restart, matching the Rust
// backend's daemon_main.rs / daemon_client.rs split).
package ptycore

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Events is how the manager reports PTY activity to its host (the app
// process re-emits these as Wails events; the daemon re-emits them as
// socket data frames to attached clients).
type Events interface {
	OnData(id string, data []byte)
	OnExit(id string)
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
}

func NewManager(events Events) *Manager {
	return &Manager{sessions: make(map[string]*session), events: events}
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

	sess := &session{id: id, cmd: c, file: f}
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
			m.events.OnData(sess.id, append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
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
	return pty.Setsize(sess.file, &pty.Winsize{Cols: cols, Rows: rows})
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
	sess, ok := m.get(id)
	if !ok {
		return "", errUnknown(id)
	}
	pgid, err := unix.IoctlGetInt(int(sess.file.Fd()), unix.TIOCGPGRP)
	if err != nil || pgid <= 0 {
		// No foreground group: the session is being torn down. Not an error the
		// caller can act on — a poll that reports nothing is the truthful answer.
		return "", nil
	}
	return processName(pgid), nil
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
