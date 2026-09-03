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
	"time"
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

	// lastSeen is the last time this session did anything: a prompt written to
	// stdin, or a line streamed back. Both matter — a long turn writes no stdin
	// but streams constantly, so tracking only writes would reap a working agent.
	lastSeen time.Time
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

	sess := &Session{ID: id, cmd: c, stdin: stdin, onLine: onLine, onExit: onExit, lastSeen: time.Now()}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			m.touch(sess)
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
	m.touch(sess)
	_, err := io.WriteString(sess.stdin, data)
	return err
}

func (m *Manager) touch(sess *Session) {
	m.mu.Lock()
	sess.lastSeen = time.Now()
	m.mu.Unlock()
}

// ReapIdle stops every session that has been silent for longer than `idle` and
// returns their ids. An agent CLI holds ~150 MB even doing nothing, so a chat
// nobody has touched in half an hour should give the memory back; the transcript
// is on disk and the next prompt resumes the session.
//
// Silence is the only signal needed: a live turn streams output continuously, so
// an in-flight agent can never look idle (no separate "is a turn running" check,
// which the frontend would have to keep in sync).
func (m *Manager) ReapIdle(idle time.Duration) []string {
	cutoff := time.Now().Add(-idle)
	m.mu.Lock()
	var stale []*Session
	for id, sess := range m.sessions {
		if sess.lastSeen.Before(cutoff) {
			stale = append(stale, sess)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	ids := make([]string, 0, len(stale))
	for _, sess := range stale {
		sess.stdin.Close()
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
		}
		ids = append(ids, sess.ID)
	}
	return ids
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

// StopAll kills every live session. Used on app shutdown so agent CLIs don't
// outlive the app as orphans.
func (m *Manager) StopAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for id, s := range m.sessions {
		sessions = append(sessions, s)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	for _, sess := range sessions {
		sess.stdin.Close()
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
		}
	}
}
