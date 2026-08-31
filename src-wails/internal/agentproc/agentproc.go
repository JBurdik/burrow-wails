// Package agentproc runs long-lived agent CLI subprocesses (Claude Code,
// Codex/ACP, ...) and streams their stdout line-by-line to a callback,
// mirroring the process-management shape of claude_start/acp_start/
// codex_start in src-tauri/src/lib.rs. It is transport/event-agnostic: the
// caller decides what event name to re-emit lines under.
package agentproc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

type Session struct {
	ID     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	onLine func(line string)
	onExit func()
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Start spawns `command args...` in cwd. onLine is called for every stdout
// line (JSONL agent event stream); onExit once the process exits.
func (m *Manager) Start(id, command string, args []string, cwd string, env []string, onLine func(string), onExit func()) error {
	c := exec.Command(command, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}

	stdin, err := c.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		return err
	}

	sess := &Session{ID: id, cmd: c, stdin: stdin, onLine: onLine, onExit: onExit}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			onLine(scanner.Text())
		}
		c.Wait()
		m.removeIfCurrent(id, sess)
		onExit()
	}()

	return nil
}

func (m *Manager) Send(id string, data string) error {
	sess, ok := m.get(id)
	if !ok {
		return fmt.Errorf("unknown agent session: %s", id)
	}
	_, err := io.WriteString(sess.stdin, data)
	return err
}

// Alive reports whether a session is currently running under this id.
func (m *Manager) Alive(id string) bool {
	_, ok := m.get(id)
	return ok
}

// Signal forwards a signal (SIGINT for a graceful turn abort) to the process.
func (m *Manager) Signal(id string, sig os.Signal) error {
	sess, ok := m.get(id)
	if !ok {
		return fmt.Errorf("unknown agent session: %s", id)
	}
	if sess.cmd.Process == nil {
		return fmt.Errorf("agent session has no process: %s", id)
	}
	return sess.cmd.Process.Signal(sig)
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown agent session: %s", id)
	}
	// Unregister synchronously so a following Start with the same id creates
	// the replacement rather than treating the terminating process as alive.
	delete(m.sessions, id)
	m.mu.Unlock()
	sess.stdin.Close()
	if sess.cmd.Process != nil {
		return sess.cmd.Process.Kill()
	}
	return nil
}

func (m *Manager) get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// removeIfCurrent removes only the exact process that registered the id. An
// old process can finish after a replacement was started with the same id.
func (m *Manager) removeIfCurrent(id string, sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[id] == sess {
		delete(m.sessions, id)
	}
}
