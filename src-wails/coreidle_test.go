package main

import (
	"testing"
	"time"
)

func TestCoreShouldExit(t *testing.T) {
	now := time.Now()
	long := now.Add(-coreIdleGrace)
	short := now.Add(-coreIdleGrace + time.Second)
	for _, tc := range []struct {
		name   string
		ui     bool
		lastUI time.Time
		work   coreWork
		want   bool
	}{
		{"idle long enough", false, long, coreWork{}, true},
		{"ui connected", true, long, coreWork{}, false},
		{"within grace", false, short, coreWork{}, false},
		{"turn open", false, long, coreWork{turnOpen: true}, false},
		{"queued", false, long, coreWork{queued: true}, false},
		{"scheduled", false, long, coreWork{scheduled: true}, false},
		{"remote access", false, long, coreWork{remoteAccess: true}, false},
		{"login item", false, long, coreWork{loginItem: true}, false},
	} {
		if got := coreShouldExit(tc.ui, tc.lastUI, now, tc.work); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
