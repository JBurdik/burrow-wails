package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"burrow/internal/daemonproto"
)

// DaemonClient talks to burrow-daemon over its Unix socket and re-emits
// PTY output as Wails events (`pty-data-{id}`), so PTYs keep running even
// if the app process restarts — same contract as pty.go's PtyManager
// offered the frontend, now backed by the daemon instead of an in-process
// manager.
type DaemonClient struct {
	ctx      context.Context
	sockPath string

	mu      sync.Mutex
	conn    net.Conn
	enc     *json.Encoder
	pending map[string]chan *daemonproto.Response
}

func NewDaemonClient(ctx context.Context, sockPath string) *DaemonClient {
	return &DaemonClient{ctx: ctx, sockPath: sockPath, pending: make(map[string]chan *daemonproto.Response)}
}

// Ensure connects to the daemon, spawning it if it isn't already running. A
// socket that answers connect() might still be an ORPHAN: a daemon process
// left running from a build or worktree that no longer exists, frozen on
// whatever code it had when it started. That is exactly how terminal status
// hooks went dark on a running app: a hook fix landed and got reinstalled,
// but every live PTY kept running under yesterday's daemon, which never
// picked it up because nothing ever checked. staleAndReplaced() catches that
// by comparing the live daemon's own binary against the one we'd spawn.
func (d *DaemonClient) Ensure() error {
	if err := d.connect(); err == nil {
		if !d.staleAndReplaced() {
			return nil
		}
		// fell through: killed the stale daemon and removed its socket, spawn below
	}
	if err := d.spawn(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	var lastErr error
	for i := 0; i < 20; i++ {
		if err := d.connect(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become reachable: %w", lastErr)
}

// staleAndReplaced compares the just-connected daemon's own binary against
// the one Ensure would spawn (expectedDaemonPath). A mismatch, or an ExePath
// that no longer exists on disk (our exact repro: a daemon built from a
// since-deleted git worktree), means the live process is orphaned code — it
// gets killed and its socket file removed so the code below spawns a current
// one. Returns false (nothing replaced) for the dev `go run` fallback, which
// has no fixed binary to compare against, and for a daemon old enough to
// predate the "version" request itself (it can't self-heal past that one
// release; every daemon after this one can).
func (d *DaemonClient) staleAndReplaced() bool {
	want, err := expectedDaemonPath()
	if err != nil {
		return false
	}
	if _, err := os.Stat(want); err != nil {
		return false // dev fallback: nothing built to compare against
	}
	resp, err := d.call(daemonproto.Request{Kind: "version"})
	if err != nil {
		return false
	}
	if resp.ExePath == want {
		if _, err := os.Stat(resp.ExePath); err == nil {
			return false // same binary, still on disk: current
		}
	}
	log.Printf("daemon: replacing stale daemon pid %d (%s), want %s", resp.Pid, resp.ExePath, want)
	d.mu.Lock()
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
		d.enc = nil
	}
	d.mu.Unlock()
	if resp.Pid > 0 {
		if p, err := os.FindProcess(resp.Pid); err == nil {
			_ = p.Kill()
		}
	}
	_ = os.Remove(d.sockPath)
	return true
}

func expectedDaemonPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), "burrow-daemon"), nil
}

func (d *DaemonClient) connect() error {
	conn, err := net.Dial("unix", d.sockPath)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.conn = conn
	d.enc = json.NewEncoder(conn)
	d.mu.Unlock()
	go d.readLoop(conn)
	return nil
}

func (d *DaemonClient) spawn() error {
	daemonPath, err := expectedDaemonPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(daemonPath); err != nil {
		// Dev fallback: run from the module's cmd/ dir via `go run`.
		cmd := exec.Command("go", "run", "./cmd/burrow-daemon")
		cmd.Env = append(os.Environ(), "BURROW_DAEMON_SOCK="+d.sockPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Start()
	}
	cmd := exec.Command(daemonPath)
	cmd.Env = append(os.Environ(), "BURROW_DAEMON_SOCK="+d.sockPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

func (d *DaemonClient) readLoop(conn net.Conn) {
	dec := json.NewDecoder(conn)
	for {
		var env daemonproto.Envelope
		if err := dec.Decode(&env); err != nil {
			return
		}
		switch env.Type {
		case "response":
			d.mu.Lock()
			ch, ok := d.pending[env.Response.ReqID]
			if ok {
				delete(d.pending, env.Response.ReqID)
			}
			d.mu.Unlock()
			if ok {
				ch <- env.Response
			}
		case "frame":
			f := env.Frame
			switch f.Event {
			case "pty-data":
				busEmit("pty-data-"+f.ID, f.Data)
			case "pty-exit":
				busEmit("pty-exit-"+f.ID, nil)
			}
		}
	}
}

func (d *DaemonClient) call(req daemonproto.Request) (*daemonproto.Response, error) {
	req.ReqID = uuid.NewString()
	ch := make(chan *daemonproto.Response, 1)

	d.mu.Lock()
	if d.enc == nil {
		d.mu.Unlock()
		return nil, fmt.Errorf("daemon not connected")
	}
	d.pending[req.ReqID] = ch
	err := d.enc.Encode(req)
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if !resp.OK {
			return resp, fmt.Errorf("%s", resp.Error)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("daemon call %q timed out", req.Kind)
	}
}

// CreatePty spawns a PTY under the caller-supplied id (the frontend owns
// its own pty-id counter; the backend never generates or picks one).
func (d *DaemonClient) CreatePty(id, cwd string, cols, rows uint16, env []string) error {
	_, err := d.call(daemonproto.Request{Kind: "spawn", ID: id, Cwd: cwd, Cols: cols, Rows: rows, Env: env})
	return err
}

func (d *DaemonClient) Write(id string, data []int) error {
	_, err := d.call(daemonproto.Request{Kind: "write", ID: id, Data: data})
	return err
}

func (d *DaemonClient) Resize(id string, cols, rows uint16) error {
	_, err := d.call(daemonproto.Request{Kind: "resize", ID: id, Cols: cols, Rows: rows})
	return err
}

func (d *DaemonClient) Kill(id string) error {
	_, err := d.call(daemonproto.Request{Kind: "kill", ID: id})
	return err
}

// Foreground asks the daemon what is running in the foreground of a PTY. Only
// the daemon can answer: it holds the master fd the answer is read from.
func (d *DaemonClient) Foreground(id string) (string, error) {
	resp, err := d.call(daemonproto.Request{Kind: "foreground", ID: id})
	if err != nil {
		return "", err
	}
	return resp.Name, nil
}

func (d *DaemonClient) List() ([]string, error) {
	resp, err := d.call(daemonproto.Request{Kind: "list"})
	if err != nil {
		return nil, err
	}
	if resp.IDs == nil {
		return []string{}, nil // never hand JS a null to .map over
	}
	return resp.IDs, nil
}
