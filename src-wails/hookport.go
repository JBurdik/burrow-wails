package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// keepHookPort re-points <app-data>/hook.port at this instance's hook server
// when the file names a port nobody listens on. Beta and stable share the data
// dir: a stable launch (in-process core, no core.lock) overwrote hook.port and
// quit, and every `burrow` verb from the still-running beta then dialled a dead
// port for an hour — the CLI trusts the file over its baked env var, because
// the env var is the one that goes stale across a restart.
//
// A port someone DOES answer on is left alone: two live backends on one data
// dir are already wrong, and flipping the file between them every 30 s would
// only make it worse.
func keepHookPort(ctx context.Context, path string, own int) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b, _ := os.ReadFile(path)
			if hookPortNeedsReclaim(string(b), own, portAnswers) {
				if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", own)), 0o644); err == nil {
					log.Printf("hook.port: reclaimed for %d (was %q)", own, strings.TrimSpace(string(b)))
				}
			}
		}
	}
}

// hookPortNeedsReclaim: the file is missing/garbage, or names another port
// that does not answer.
func hookPortNeedsReclaim(content string, own int, answers func(int) bool) bool {
	p, err := strconv.Atoi(strings.TrimSpace(content))
	if err != nil || p <= 0 {
		return true
	}
	return p != own && !answers(p)
}

func portAnswers(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
