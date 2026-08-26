package main

import (
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"burrow/internal/agentproc"
)

// Claude Code, ACP (Codex, etc) and LSP subprocess bindings. Each spawns a
// long-lived agent CLI, streams its stdout as newline-JSON events (same
// `claude-data-{id}` / `acp-data-{id}` event names the frontend already
// listens for), and accepts writes back to stdin — matching claude_start/
// claude_send/claude_stop and acp_start/acp_send/acp_stop/codex_start/
// codex_send/codex_stop in src-tauri/src/lib.rs.

func (a *App) claudeMgr() *agentproc.Manager {
	if a.claudeAgents == nil {
		a.claudeAgents = agentproc.NewManager()
	}
	return a.claudeAgents
}

func (a *App) acpMgr() *agentproc.Manager {
	if a.acpAgents == nil {
		a.acpAgents = agentproc.NewManager()
	}
	return a.acpAgents
}

// --- Claude Code ---

func (a *App) ClaudeStart(cwd string, args []string) (string, error) {
	id := uuid.NewString()
	fullArgs := append([]string{"--output-format", "stream-json", "--input-format", "stream-json"}, args...)
	err := a.claudeMgr().Start(id, "claude", fullArgs, cwd, nil,
		func(line string) { runtime.EventsEmit(a.ctx, "claude-data-"+id, line) },
		func() { runtime.EventsEmit(a.ctx, "claude-data-"+id, `{"type":"exit"}`) },
	)
	return id, err
}

func (a *App) ClaudeSend(id, text string) error {
	return a.claudeMgr().Send(id, text+"\n")
}

func (a *App) ClaudeStop(id string) error {
	return a.claudeMgr().Stop(id)
}

func (a *App) ClaudeAbort(id string) error {
	return a.claudeMgr().Stop(id)
}

// --- ACP (Codex etc) ---

func (a *App) AcpStart(command string, cwd string, args []string) (string, error) {
	id := uuid.NewString()
	err := a.acpMgr().Start(id, command, args, cwd, nil,
		func(line string) { runtime.EventsEmit(a.ctx, "acp-data-"+id, line) },
		func() { runtime.EventsEmit(a.ctx, "acp-data-"+id, `{"type":"exit"}`) },
	)
	return id, err
}

func (a *App) CodexStart(cwd string, args []string) (string, error) {
	return a.AcpStart("codex", cwd, args)
}

func (a *App) AcpSend(id, text string) error {
	return a.acpMgr().Send(id, text+"\n")
}

func (a *App) CodexSend(id, text string) error {
	return a.AcpSend(id, text)
}

func (a *App) AcpStop(id string) error {
	return a.acpMgr().Stop(id)
}

func (a *App) CodexStop(id string) error {
	return a.acpMgr().Stop(id)
}
