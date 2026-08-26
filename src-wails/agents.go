package main

import "burrow/internal/agentproc"

// Agent subprocess managers. The Claude Code chat runtime lives in
// claudechat.go; the ACP / Codex app-server bridge in acp.go (it owns its own
// JSON-RPC session registry, so it doesn't use agentproc).

func (a *App) claudeMgr() *agentproc.Manager {
	if a.claudeAgents == nil {
		a.claudeAgents = agentproc.NewManager()
	}
	return a.claudeAgents
}
