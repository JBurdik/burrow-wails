package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// LSP subprocess management — matches lsp_start/lsp_send/lsp_stop in
// src-tauri/src/lib.rs. Frames are passed through verbatim (JSON-RPC over
// stdio, LSP already frames its own messages with Content-Length headers)
// and re-emitted as `lsp-msg-{id}` events.
type lspSession struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

type lspManager struct {
	mu       sync.Mutex
	sessions map[string]*lspSession
}

func newLSPManager() *lspManager {
	return &lspManager{sessions: make(map[string]*lspSession)}
}

func (a *App) lsp() *lspManager {
	if a.lspMgr == nil {
		a.lspMgr = newLSPManager()
	}
	return a.lspMgr
}

func (a *App) LspStart(id, command string, args []string, cwd string) error {
	c := exec.Command(command, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	stdin, err := c.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	if err := c.Start(); err != nil {
		return err
	}

	m := a.lsp()
	m.mu.Lock()
	m.sessions[id] = &lspSession{cmd: c, stdin: stdin}
	m.mu.Unlock()

	go func() {
		r := bufio.NewReaderSize(stdout, 64*1024)
		buf := make([]byte, 64*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				runtime.EventsEmit(a.ctx, "lsp-msg-"+id, string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()

	return nil
}

func (a *App) LspSend(id, message string) error {
	m := a.lsp()
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return errUnknownLSP(id)
	}
	_, err := io.WriteString(sess.stdin, message)
	return err
}

func (a *App) LspStop(id string) error {
	m := a.lsp()
	m.mu.Lock()
	sess, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	sess.stdin.Close()
	if sess.cmd.Process != nil {
		return sess.cmd.Process.Kill()
	}
	return nil
}

func errUnknownLSP(id string) error {
	return fmt.Errorf("unknown lsp session: %s", id)
}
