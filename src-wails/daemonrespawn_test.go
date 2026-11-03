package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"burrow/internal/daemonproto"
)

// A daemon that dies under a running app is respawned on the next call, and
// Restart replaces a live one with a new pid.
func TestDaemonRespawnAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds burrow-daemon")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(filepath.Dir(exe), "burrow-daemon")
	if out, err := exec.Command("go", "build", "-o", bin, "./cmd/burrow-daemon").CombinedOutput(); err != nil {
		t.Fatalf("build daemon: %v\n%s", err, out)
	}
	sock := filepath.Join(t.TempDir(), "d.sock")
	d := NewDaemonClient(context.Background(), sock)

	// No daemon at all: the first call must spawn one instead of failing.
	resp, err := d.call(daemonproto.Request{Kind: "version"})
	if err != nil {
		t.Fatalf("call with no daemon: %v", err)
	}
	first := resp.Pid
	t.Cleanup(func() {
		if r, err := d.call(daemonproto.Request{Kind: "version"}); err == nil {
			if p, err := os.FindProcess(r.Pid); err == nil {
				_ = p.Kill()
			}
		}
	})

	pid, err := d.Restart()
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if pid == 0 || pid == first {
		t.Fatalf("restart pid = %d, first = %d", pid, first)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(sock), "daemon.log")); err != nil {
		t.Fatalf("daemon.log not written: %v", err)
	}
}
