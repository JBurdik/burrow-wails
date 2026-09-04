package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code chat runtime. Ports claude_start/claude_send/claude_stop/
// claude_abort from src-tauri/src/lib.rs: the CLI runs in stream-json mode on
// both ends, every stdout line is re-emitted as `claude-data-{id}` where {id}
// is the FRONTEND's chat id (ClaudeChat.vue listens on exactly that name), and
// input is a stream-json `user` message on stdin.

// resolveAgentBin mirrors resolve_lsp_bin: a GUI-launched app has a bare PATH,
// so look through the usual toolchain dirs before trusting PATH.
func resolveAgentBin(name, root string) string {
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	dirs := []string{filepath.Join(root, "node_modules/.bin")}
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{".cargo/bin", ".npm-global/bin", ".local/bin", ".volta/bin"} {
			dirs = append(dirs, filepath.Join(home, d))
		}
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin")
	dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
	for _, d := range dirs {
		c := filepath.Join(d, name)
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func augmentedPath(root string) string {
	parts := []string{filepath.Join(root, "node_modules/.bin")}
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{".cargo/bin", ".volta/bin", ".local/bin", ".npm-global/bin"} {
			parts = append(parts, filepath.Join(home, d))
		}
	}
	parts = append(parts, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	if p := os.Getenv("PATH"); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// buildMcpConfig passes the user's own stdio MCP servers (from
// <CLAUDE_CONFIG_DIR|~/.claude>/settings.json) through to the chat session, plus
// Burrow's own control server so a chat agent gets typed app tools instead of
// having to shell out. Remote servers are dropped — they hang without a TTY.
func buildMcpConfig() string {
	empty := mustJSON(map[string]any{"mcpServers": burrowMcpServers(nil)})
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return empty
		}
		dir = filepath.Join(home, ".claude")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return empty
	}
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil {
		return empty
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	local := map[string]any{}
	for name, v := range servers {
		if m, ok := v.(map[string]any); ok {
			if t, ok := m["type"].(string); ok && t != "stdio" {
				continue
			}
		}
		local[name] = v
	}
	return mustJSON(map[string]any{"mcpServers": burrowMcpServers(local)})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"mcpServers":{}}`
	}
	return string(b)
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// ClaudeStart spawns the Claude Code CLI for chat `id`. Already-running ids are
// a no-op (matching the Rust behaviour ClaudeChat.vue relies on when it
// remounts).
func (a *App) ClaudeStart(id, cwd, resumeSessionID, permissionMode, appendSystemPrompt, model, effort, configDir, profileCommand, profileArgs string) error {
	if a.claudeMgr().Alive(id) {
		return nil
	}
	cmdName := "claude"
	if s := strings.TrimSpace(profileCommand); s != "" {
		cmdName = s
	}
	bin := resolveAgentBin(cmdName, cwd)
	if bin == "" {
		return fmt.Errorf("%s binary not found (checked ~/.local/bin, homebrew, PATH)", cmdName)
	}

	perm := "default"
	if oneOf(permissionMode, "acceptEdits", "bypassPermissions", "plan", "auto", "dontAsk") {
		perm = permissionMode
	}
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--input-format", "stream-json",
		"--include-partial-messages",
		"--permission-mode", perm,
		// Hidden flag: routes every permission / blocking-tool decision to us as a
		// can_use_tool control_request on stdin instead of the CLI deciding itself.
		// Without it Edit/Write/Bash/ExitPlanMode/AskUserQuestion never reach the UI.
		"--permission-prompt-tool", "stdio",
		"--mcp-config", buildMcpConfig(),
	}
	if resumeSessionID != "" {
		args = append(args, "--resume", resumeSessionID)
	}
	if s := strings.TrimSpace(appendSystemPrompt); s != "" {
		args = append(args, "--append-system-prompt", s)
	}
	// Allowlisted so the model id can't smuggle extra argv.
	if strings.HasPrefix(model, "claude-") {
		args = append(args, "--model", model)
	}
	if oneOf(effort, "low", "medium", "high", "xhigh", "max") {
		args = append(args, "--effort", effort)
	}
	for _, flag := range strings.Fields(profileArgs) {
		args = append(args, flag)
	}

	// Env overrides (os/exec keeps the LAST duplicate, so these win over the
	// inherited environment). Empty ANTHROPIC_API_KEY keeps the session on the
	// user's subscription/OAuth auth instead of billed API-key usage.
	env := []string{
		"ANTHROPIC_API_KEY=",
		"PATH=" + a.burrowBinDir + string(os.PathListSeparator) + augmentedPath(cwd),
		"BURROW_SESSION_DIR=" + a.sessionDir,
		"BURROW_CWD=" + cwd,
	}
	if a.hookSrv != nil {
		env = append(env, fmt.Sprintf("BURROW_HOOK_PORT=%d", a.hookSrv.port))
	}
	if dir, err := appDataDir(); err == nil {
		env = append(env, "BURROW_HOME_DIR="+dir)
	}
	if cd := strings.TrimSpace(configDir); cd != "" {
		if strings.HasPrefix(cd, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				cd = filepath.Join(home, cd[2:])
			}
		}
		env = append(env, "CLAUDE_CONFIG_DIR="+cd)
	}
	// NOTE: deliberately no BURROW_PTY_ID — a chat is not a tab, so the global
	// status hook stays a no-op for it.

	return a.claudeMgr().Start(id, bin, args, cwd, env,
		func(line string) { a.emitChatLine(id, "claude-data", line) },
		func() { a.emitChatLine(id, "claude-data", `{"type":"exit"}`) },
	)
}

// ClaudeSend wraps the prompt in the stream-json `user` envelope the CLI reads
// on stdin. Images are data URIs ("data:<mime>;base64,<data>").
func (a *App) ClaudeSend(id, text, sessionID string, images []string) error {
	content := []any{}
	for _, uri := range images {
		rest, ok := strings.CutPrefix(uri, "data:")
		if !ok {
			continue
		}
		meta, data, ok := strings.Cut(rest, ",")
		if !ok {
			continue
		}
		mediaType := "image/png"
		if m, _, _ := strings.Cut(meta, ";"); m != "" {
			mediaType = m
		}
		content = append(content, map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": mediaType, "data": data},
		})
	}
	content = append(content, map[string]any{"type": "text", "text": text})

	msg, err := json.Marshal(map[string]any{
		"type":       "user",
		"session_id": sessionID,
		"message":    map[string]any{"role": "user", "content": content},
	})
	if err != nil {
		return err
	}
	return a.claudeWrite(id, string(msg))
}

// claudeWrite puts one raw JSON line on the CLI's stdin (control responses are
// already fully-formed, so they must not be wrapped as a user message).
func (a *App) claudeWrite(id, line string) error {
	return a.claudeMgr().Send(id, line+"\n")
}

func (a *App) ClaudeStop(id string) error {
	return a.claudeMgr().Stop(id)
}

// ClaudeAbort interrupts the current turn with SIGINT so the CLI finalizes
// gracefully (it emits a result event); SIGKILL would just drop the pipe.
func (a *App) ClaudeAbort(id string) error {
	if err := a.claudeMgr().Signal(id, os.Interrupt); err != nil {
		// No live process (or signal refused) — fall back to a hard stop so the
		// UI's abort button is never a no-op.
		return a.claudeMgr().Stop(id)
	}
	return nil
}
