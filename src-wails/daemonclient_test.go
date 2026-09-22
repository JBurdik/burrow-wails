package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"burrow/internal/daemonproto"
)

// The daemon drops a client whose write blocked too long (daemonserver's
// clientWriteTimeout). The client must notice and redial on the next call —
// before this it kept writing into the dead socket, so every terminal went
// silent and new tabs never spawned a PTY until the app restarted.
func TestDaemonClientReconnectsAfterServerDrop(t *testing.T) {
	// Not t.TempDir(): its path blows the ~104-byte sun_path limit.
	dir, err := os.MkdirTemp("", "bd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "d.sock")
	ln, lerr := net.Listen("unix", sock)
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer ln.Close()

	// Answers one request, then hangs up — the drop we are recovering from.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var req daemonproto.Request
				if err := json.NewDecoder(c).Decode(&req); err != nil {
					return
				}
				_ = json.NewEncoder(c).Encode(daemonproto.Envelope{
					Type:     "response",
					Response: &daemonproto.Response{ReqID: req.ReqID, OK: true, IDs: []string{"7"}},
				})
			}(conn)
		}
	}()

	d := NewDaemonClient(context.Background(), sock)
	if err := d.connect(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.List(); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call lands after the server hung up: it must redial, not time out.
	ids, err := d.List()
	if err != nil {
		t.Fatalf("call after server drop: %v", err)
	}
	if len(ids) != 1 || ids[0] != "7" {
		t.Fatalf("got %v", ids)
	}
}
