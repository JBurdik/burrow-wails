package main

import (
	"fmt"
	"os"
	"time"
)

// AcpCapabilities is what an ACP adapter's session/new reports: the selector
// sets (modes + configOptions — model, effort, …) the composer offers.
type AcpCapabilities struct {
	Modes         any `json:"modes"`
	ConfigOptions any `json:"configOptions"`
}

// AcpProbe starts a generic ACP provider instance just long enough to read its
// selectors, so the welcome composer can offer them before any chat exists —
// the ACP twin of CodexListModels. The session is never prompted.
// ponytail: a throwaway session/new per probe; the client caches the answer.
func (a *App) AcpProbe(agentID, cwd string) (AcpCapabilities, error) {
	var out AcpCapabilities
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	opts, err := a.rpcLaunchFor(Chat{AgentKind: agentID}, "acp", cwd)
	if err != nil {
		return out, err
	}
	if opts.OpenAI != nil || opts.Kind == "codex" {
		return out, fmt.Errorf("%s is not a generic ACP adapter", agentID)
	}
	return probeACP(opts, cwd)
}

// probeACP spawns opts' adapter, reads session/new's selectors, kills it.
func probeACP(opts AcpStartOpts, cwd string) (AcpCapabilities, error) {
	var out AcpCapabilities
	bin := resolveAgentBin(opts.Command, cwd)
	if bin == "" {
		return out, fmt.Errorf("%s binary not found (checked ~/.local/bin, homebrew, PATH)", opts.Command)
	}
	env := map[string]string{"PATH": augmentedPath(cwd)}
	for k, v := range opts.Env {
		env[k] = v
	}
	cmd, stdin, reader, stderrTail, err := spawnStdio(bin, opts.Args, cwd, env)
	if err != nil {
		return out, fmt.Errorf("failed to spawn acp adapter: %w", err)
	}
	// Killing the child is also how a blocked read unblocks.
	stop := time.AfterFunc(20*time.Second, func() { _ = killAgentProcessTree(cmd) })
	defer func() {
		stop.Stop()
		_ = stdin.Close()
		_ = killAgentProcessTree(cmd)
	}()
	fail := func(err error) (AcpCapabilities, error) {
		if tail := stderrTail(); tail != "" {
			return out, fmt.Errorf("%w\n--- adapter stderr ---\n%s", err, tail)
		}
		return out, err
	}
	sess := &acpSession{stdin: stdin, proto: protoACP}
	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"protocolVersion":    1,
			"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": false},
			"clientInfo":         map[string]any{"name": "burrow", "title": "Burrow", "version": appVersion},
		},
	}); err != nil {
		return fail(err)
	}
	if _, err := reader.await(0, nil); err != nil {
		return fail(err)
	}
	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "session/new",
		"params": map[string]any{"cwd": cwd, "mcpServers": []any{}},
	}); err != nil {
		return fail(err)
	}
	resp, err := reader.await(1, nil)
	if err != nil {
		return fail(err)
	}
	if e, bad := resp["error"]; bad {
		return fail(fmt.Errorf("session/new: %v", e))
	}
	result := mapOf(resp["result"])
	out.Modes, out.ConfigOptions = result["modes"], result["configOptions"]
	if out.ConfigOptions == nil {
		out.ConfigOptions = []any{}
	}
	return out, nil
}
