package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"burrow/internal/agentphase"
)

// claudeSessionStatus is the part of Claude Code's own per-process registry
// file (<config>/sessions/<pid>.json, Claude >= 2.1.139) the phase poll reads.
// Observed on 2.1.291: the file is keyed by the claude process pid, written
// while the process lives and unlinked on a clean exit (SIGTERM, /exit) — a
// SIGKILL leaves it behind. `status` is idle | busy | waiting (the claude-
// native docs also list `shell`); `waitingFor` is present only with `waiting`
// ("permission prompt" observed). The file is an undocumented internal, so
// every failure to read it means "no news", never an error.
type claudeSessionStatus struct {
	PID        int    `json:"pid"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
}

// claudeSessionsDir mirrors Claude's own resolution: CLAUDE_CONFIG_DIR, else
// ~/.claude. Read from Burrow's environment — the PTY inherits it unless the
// user's shell rc overrides it, in which case the file is simply not found and
// the hook/foreground logic carries on.
func claudeSessionsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "sessions")
}

// readClaudeStatus reads <dir>/<pid>.json. ok is false for a missing,
// half-written or foreign file (wrong pid/kind), so a stale file left by a
// killed claude can never be attributed to an unrelated process.
func readClaudeStatus(dir string, pid int) (claudeSessionStatus, bool) {
	if dir == "" || pid <= 0 {
		return claudeSessionStatus{}, false
	}
	raw, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(pid)+".json"))
	if err != nil {
		return claudeSessionStatus{}, false
	}
	var st claudeSessionStatus
	if json.Unmarshal(raw, &st) != nil || st.PID != pid || st.Kind != "interactive" {
		return claudeSessionStatus{}, false
	}
	return st, true
}

// event maps the file's status onto the phase vocabulary. Same channel as the
// hooks (they stay authoritative for the *content* of a turn end — model,
// title, failure detail); the file only adds what hooks miss and corrects a
// lost Stop. `shell` (turn ended, background shell alive) is idle for the
// agent loop. ok is false for an unknown literal: the vocabulary can grow.
func (s claudeSessionStatus) event() (agentphase.Event, bool) {
	switch s.Status {
	case "busy":
		return agentphase.Event{Kind: agentphase.HookRunning}, true
	case "waiting":
		if s.WaitingFor == "permission prompt" {
			return agentphase.Event{Kind: agentphase.HookPermission}, true
		}
		return agentphase.Event{Kind: agentphase.HookWaiting}, true
	case "idle", "shell":
		return agentphase.Event{Kind: agentphase.StatusIdle}, true
	}
	return agentphase.Event{}, false
}
