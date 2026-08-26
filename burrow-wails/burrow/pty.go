package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func errUnknownPty(id string) error {
	return fmt.Errorf("unknown pty id: %s", id)
}

// PtyManager owns every live PTY session, keyed by id. Mirrors the Rust
// backend's create_pty/write_pty/resize_pty/kill_pty command surface
// (src-tauri/src/lib.rs) — same event name (`pty-data-{id}`) so the
// existing frontend listeners keep working once wired up.
type PtyManager struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
}

type ptySession struct {
	id   string
	cmd  *exec.Cmd
	file *os.File
}

func NewPtyManager() *PtyManager {
	return &PtyManager{sessions: make(map[string]*ptySession)}
}

// CreatePty spawns shell/cmd in a PTY and starts streaming its output as
// `pty-data-{id}` events. Returns the new session id.
func (m *PtyManager) CreatePty(ctx context.Context, shell string, args []string, cwd string, env []string) (string, error) {
	id := uuid.NewString()

	c := exec.Command(shell, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}

	f, err := pty.Start(c)
	if err != nil {
		return "", err
	}

	sess := &ptySession{id: id, cmd: c, file: f}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go m.pump(ctx, sess)

	return id, nil
}

// pump reads PTY output and emits it as `pty-data-{id}` events, matching
// the OSC/event contract XTerm.vue already listens for.
func (m *PtyManager) pump(ctx context.Context, sess *ptySession) {
	buf := make([]byte, 32*1024)
	eventName := "pty-data-" + sess.id
	for {
		n, err := sess.file.Read(buf)
		if n > 0 {
			runtime.EventsEmit(ctx, eventName, string(buf[:n]))
		}
		if err != nil {
			break
		}
	}
	m.mu.Lock()
	delete(m.sessions, sess.id)
	m.mu.Unlock()
	runtime.EventsEmit(ctx, "pty-exit-"+sess.id, nil)
}

func (m *PtyManager) Write(id string, data string) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknownPty(id)
	}
	_, err := sess.file.Write([]byte(data))
	return err
}

func (m *PtyManager) Resize(id string, cols, rows uint16) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknownPty(id)
	}
	return pty.Setsize(sess.file, &pty.Winsize{Cols: cols, Rows: rows})
}

func (m *PtyManager) Kill(id string) error {
	sess, ok := m.get(id)
	if !ok {
		return errUnknownPty(id)
	}
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
	return sess.file.Close()
}

func (m *PtyManager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (m *PtyManager) get(id string) (*ptySession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}
