//go:build !headless

package main

import (
	"testing"

	"burrow/internal/agentphase"
)

func TestTrayCountsOnlyWaitingPhases(t *testing.T) {
	phases := map[string]agentphase.Phase{
		"pty:1":  {State: agentphase.WaitingInput},
		"chat:2": {State: agentphase.WaitingApproval},
		"chat:3": {State: agentphase.Running},
		"pty:4":  {State: agentphase.Done},
	}
	if n := waitingCount(phases); n != 2 {
		t.Fatalf("waitingCount = %d, want 2", n)
	}
	if l := trayLabel(0); l != "" {
		t.Fatalf("trayLabel(0) = %q, want empty (icon only)", l)
	}
	if l := trayLabel(2); l != "2" {
		t.Fatalf("trayLabel(2) = %q", l)
	}
}
