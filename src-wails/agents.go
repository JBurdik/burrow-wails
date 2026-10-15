package main

import (
	"os"

	"burrow/internal/agentproc"
)

// Agent subprocess managers. The Claude Code chat runtime lives in
// claudechat.go + chatruntime.go; the ACP / Codex app-server bridge in acp.go
// (it owns its own JSON-RPC session registry, so it doesn't use agentproc).

// chatProcs is the process starter the Claude chat runtime drives. Two
// adapters: *agentproc.Manager (real) and the fake in chatruntime_test.go.
type chatProcs interface {
	Start(id, command string, args []string, cwd string, env []string, onLine func(string), onExit func()) error
	Send(id, data string) error
	Stop(id string) error
	Signal(id string, sig os.Signal) error
	Alive(id string) bool
}

func (a *App) claudeMgr() chatProcs {
	if a.claudeProcs != nil {
		return a.claudeProcs
	}
	if a.claudeAgents == nil {
		a.claudeAgents = agentproc.NewManager()
	}
	return a.claudeAgents
}
