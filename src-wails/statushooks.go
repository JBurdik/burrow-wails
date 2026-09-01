package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Global agent status hooks — the primary source of tab status dots.
//
// Each agent CLI gets a hook entry running `burrow hook`, which no-ops unless
// BURROW_PTY_ID is set (i.e. outside a Burrow PTY). That's what makes status
// work for every session — launched by button, typed by hand, or reattached
// after a restart — instead of only for the ones Burrow spawns itself. Ported
// from install_status_hooks/merge_status_hooks in src-tauri/src/lib.rs.
//
// Claude: <dir>/settings.json. Codex: <dir>/hooks.json (same schema, fewer
// events — SessionStart/StopFailure/Notification-type are Claude-only).
// Copilot: its own file per config at <dir>/hooks/burrow.json, camelCase
// events, "bash" not "command", so the state is baked into each command and
// we own (and delete) the file wholesale.

var claudeHookEvents = []string{
	"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "PermissionRequest",
	"SessionStart", "StopFailure", "Notification",
}

var codexHookEvents = []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"}

// hookDirs is where hooks get installed: the default config dir per agent, any
// dir the app inherited via env at launch, plus every per-instance
// CLAUDE_CONFIG_DIR configured in Settings → Providers (read straight from
// config.json — that editor is the single place config dirs are set).
func hookDirs() (claude, codex, copilot []string) {
	home, _ := os.UserHomeDir()
	claude = []string{filepath.Join(home, ".claude"), os.Getenv("CLAUDE_CONFIG_DIR")}
	codex = []string{filepath.Join(home, ".codex"), os.Getenv("CODEX_HOME")}
	copilot = []string{filepath.Join(home, ".copilot"), os.Getenv("COPILOT_HOME")}
	claude = append(claude, providerConfigDirs()...)
	return dedup(claude), dedup(codex), dedup(copilot)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// providerConfigDirs pulls providers[].configDir out of config.json — the
// Settings → Providers instance editor writes it there.
func providerConfigDirs() []string {
	path, err := configFilePath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Providers []struct {
			ConfigDir string `json:"configDir"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	var dirs []string
	for _, p := range cfg.Providers {
		if p.ConfigDir != "" {
			dirs = append(dirs, expandTilde(p.ConfigDir))
		}
	}
	return dirs
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// installStatusHooks writes the hook entry into every known config dir.
// Idempotent: mergeStatusHooks skips a dir that already has ours.
func installStatusHooks(dataDir string) {
	burrow := filepath.Join(dataDir, "bin", "burrow")
	// Single-quoted: the macOS app-data path contains "Application Support".
	cmd := fmt.Sprintf(`[ -n "$BURROW_PTY_ID" ] && '%s' hook || true`, burrow)

	claude, codex, copilot := hookDirs()
	for _, d := range claude {
		mergeStatusHooks(filepath.Join(d, "settings.json"), claudeHookEvents, cmd, true)
	}
	for _, d := range codex {
		mergeStatusHooks(filepath.Join(d, "hooks.json"), codexHookEvents, cmd, false)
	}
	for _, d := range copilot {
		writeCopilotHooks(filepath.Join(d, "hooks", "burrow.json"), burrow)
	}
}

func uninstallStatusHooks() {
	claude, codex, copilot := hookDirs()
	for _, d := range claude {
		unmergeStatusHooks(filepath.Join(d, "settings.json"))
	}
	for _, d := range codex {
		unmergeStatusHooks(filepath.Join(d, "hooks.json"))
	}
	for _, d := range copilot {
		_ = os.Remove(filepath.Join(d, "hooks", "burrow.json"))
	}
}

// isBurrowHook recognises our own entry by the BURROW_PTY_ID + hook marker, so
// a merge never duplicates it and an unmerge never touches somebody else's
// hooks (the user's own, Superset's, herdr's…).
func isBurrowHook(group any) bool {
	g, ok := group.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := g["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		c, _ := hm["command"].(string)
		if strings.Contains(c, "BURROW_PTY_ID") && strings.Contains(c, "hook") {
			return true
		}
	}
	return false
}

// mergeStatusHooks appends our hook to each event array in a Claude/Codex-schema
// hook file. Non-destructive: absent/empty file → create, unparseable → skip
// (never clobber a file we can't read), and the original is backed up first.
func mergeStatusHooks(path string, events []string, cmd string, setShiftEnter bool) {
	existing, _ := os.ReadFile(path)
	root := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			log.Printf("status hooks: skipping unparseable %s: %v", path, err)
			return
		}
	}

	changed := false
	if setShiftEnter && root["shiftEnterKeyBindingInstalled"] != true {
		root["shiftEnterKeyBindingInstalled"] = true
		changed = true
	}

	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	for _, ev := range events {
		arr, _ := hooks[ev].([]any)
		present := false
		for _, g := range arr {
			if isBurrowHook(g) {
				present = true
				break
			}
		}
		if present {
			continue
		}
		hooks[ev] = append(arr, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": cmd}},
		})
		changed = true
	}
	if !changed {
		return
	}
	if len(existing) > 0 {
		_ = os.WriteFile(path+".burrow-bak", existing, 0o644)
	}
	writeJSONFile(path, root)
}

// unmergeStatusHooks drops only our own entries, leaving every other hook.
func unmergeStatusHooks(path string) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return
	}
	root := map[string]any{}
	if err := json.Unmarshal(existing, &root); err != nil {
		return
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return
	}
	changed := false
	for ev, v := range hooks {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, g := range arr {
			if isBurrowHook(g) {
				changed = true
				continue
			}
			kept = append(kept, g)
		}
		hooks[ev] = kept
	}
	if !changed {
		return
	}
	_ = os.WriteFile(path+".burrow-bak", existing, 0o644)
	writeJSONFile(path, root)
}

// writeCopilotHooks writes Copilot's dedicated hook file. Its schema has one
// array per event, so the state is baked into the command instead of routing
// through `burrow hook` + a stdin parse. `notification` fires when Copilot
// needs the user (permission/input prompt), not on a normal turn — hence
// `waiting`, mirroring Claude's Notification handling.
func writeCopilotHooks(path, burrow string) {
	bash := func(state string) []any {
		return []any{map[string]any{
			"type":       "command",
			"bash":       fmt.Sprintf(`[ -n "$BURROW_PTY_ID" ] && '%s' status %s || true`, burrow, state),
			"timeoutSec": 5,
		}}
	}
	writeJSONFile(path, map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"userPromptSubmitted": bash("running"),
			"preToolUse":          bash("running"),
			"postToolUse":         bash("running"),
			"notification":        bash("waiting"),
			"agentStop":           bash("done"),
			"sessionEnd":          bash("done"),
		},
	})
}

func writeJSONFile(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Printf("status hooks: write %s: %v", path, err)
	}
}

// ReinstallStatusHooks re-writes the burrow bin, hooks and agent docs
// (idempotent), for picking up a new config dir — or repairing a clobbered
// config — without an app restart.
func (a *App) ReinstallStatusHooks() {
	dataDir, err := appDataDir()
	if err != nil {
		return
	}
	if _, err := ensureBurrowBin(dataDir); err != nil {
		log.Printf("ensure burrow bin: %v", err)
	}
	installStatusHooks(dataDir)
	installAgentDocs()
}

// RemoveStatusHooks takes Burrow's hooks back out of every agent config.
func (a *App) RemoveStatusHooks() {
	uninstallStatusHooks()
}

// RepairAgentStatus force-reclaims hook.port for THIS instance's live port
// (rescues reattached PTYs whose baked BURROW_HOOK_PORT went stale across a
// restart) and re-installs the hooks. Returns the live port for UI feedback.
func (a *App) RepairAgentStatus() int {
	port := a.GetHookServerPort()
	dataDir, err := appDataDir()
	if err != nil {
		return port
	}
	if port != 0 {
		_ = os.MkdirAll(dataDir, 0o755)
		_ = os.WriteFile(filepath.Join(dataDir, "hook.port"), []byte(fmt.Sprintf("%d", port)), 0o644)
	}
	a.ReinstallStatusHooks()
	return port
}
