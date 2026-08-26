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
	"github.com/google/uuid"
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

// Create spawns shell(args...) in cwd with extra env and starts streaming
// its output via Events.OnData. Returns the new session id.
func (m *Manager) Create(shell string, args []string, cwd string, env []string) (string, error) {
	id := uuid.NewString()

	c := exec.Command(shell, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	// BURROW_PTY_ID lets the `burrow` CLI (and status hooks) inside this
	// session address itself — set here since only the manager knows the
	// id before the process actually starts.
	c.Env = append(append(os.Environ(), env...), "BURROW_PTY_ID="+id)

	f, err := pty.Start(c)
	if err != nil {
		return "", err
	}

	sess := &session{id: id, cmd: c, file: f}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.pump(sess)

	return id, nil
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
