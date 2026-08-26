// Package daemonserver runs the burrow-daemon Unix-socket server: it owns
// the real ptycore.Manager so PTYs survive the app process restarting, and
// fans out PTY output to every attached client as daemonproto Frames.
package daemonserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"burrow/internal/daemonproto"
	"burrow/internal/ptycore"
)

type Server struct {
	socketPath string
	mgr        *ptycore.Manager

	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	enc *json.Encoder
	mu  sync.Mutex
}

func (c *client) send(env daemonproto.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(env)
}

func New(socketPath string) *Server {
	s := &Server{socketPath: socketPath, clients: make(map[*client]struct{})}
	s.mgr = ptycore.NewManager(s)
	return s
}

// OnData / OnExit implement ptycore.Events, broadcasting to every client.
func (s *Server) OnData(id string, data []byte) {
	s.broadcast(daemonproto.Envelope{Type: "frame", Frame: &daemonproto.Frame{Event: "pty-data", ID: id, Data: daemonproto.BytesToInts(data)}})
}

func (s *Server) OnExit(id string) {
	s.broadcast(daemonproto.Envelope{Type: "frame", Frame: &daemonproto.Frame{Event: "pty-exit", ID: id}})
}

func (s *Server) broadcast(env daemonproto.Envelope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		c.send(env)
	}
}

// ListenAndServe removes any stale socket file, listens, and blocks
// accepting connections until the listener errors (e.g. process exit).
func (s *Server) ListenAndServe() error {
	_ = os.Remove(s.socketPath)
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	c := &client{enc: json.NewEncoder(conn)}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		conn.Close()
	}()

	dec := json.NewDecoder(bufio.NewReader(conn))
	for {
		var req daemonproto.Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		c.send(daemonproto.Envelope{Type: "response", Response: s.handle(req)})
	}
}

func (s *Server) handle(req daemonproto.Request) *daemonproto.Response {
	resp := &daemonproto.Response{ReqID: req.ReqID}
	var err error

	switch req.Kind {
	case "spawn":
		err = s.mgr.Create(req.ID, req.Cwd, req.Cols, req.Rows, req.Env)
	case "write":
		err = s.mgr.Write(req.ID, daemonproto.IntsToBytes(req.Data))
	case "resize":
		err = s.mgr.Resize(req.ID, req.Cols, req.Rows)
	case "kill":
		err = s.mgr.Kill(req.ID)
	case "list":
		resp.IDs = s.mgr.List()
	default:
		err = fmt.Errorf("unknown request kind: %s", req.Kind)
	}

	if err != nil {
		resp.Error = err.Error()
		log.Printf("daemon: %s failed: %v", req.Kind, err)
		return resp
	}
	resp.OK = true
	return resp
}
