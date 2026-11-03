package main

import (
	"sync"

	"burrow/internal/agentphase"
	"burrow/internal/daemonproto"
	"burrow/internal/progstatus"
)

// newProgramStatusTap feeds OSC 7501 reports in a PTY's output into its
// phase (internal/progstatus). One scanner per pty so a sequence split across
// frames still parses; dropped on pty-exit (ids are reused).
func newProgramStatusTap(apply func(key string, ev agentphase.Event)) func(id string, data []int) {
	var mu sync.Mutex
	scan := map[string]*progstatus.Scanner{}
	return func(id string, data []int) {
		mu.Lock()
		defer mu.Unlock()
		if data == nil {
			delete(scan, id)
			return
		}
		s := scan[id]
		if s == nil {
			s = &progstatus.Scanner{}
			scan[id] = s
		}
		for _, body := range s.Feed(daemonproto.IntsToBytes(data)) {
			if r, ok := progstatus.Parse(body); ok {
				if ev, ok := progstatus.Event(r); ok {
					apply("pty:"+id, ev)
				}
			}
		}
	}
}
