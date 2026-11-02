package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Launch settings: everything a chat's agent process is started with, stored on
// the chat row (docs/plans/006-chat-runtime.md, decisions 2/4/9).
//
// This file is groundwork — nothing calls resolveClaudeLaunch yet. The Go chat
// runtime (PR2) will, at every start, so a chat starts the same way whether a
// desktop view, the phone or `spawn` asked for it.

// ClaudeLaunch is the resolved claude_start payload, minus id/cwd/resume.
// Field names follow ClaudeStart's parameters. "" = not set; the CLI default
// applies.
type ClaudeLaunch struct {
	Model              string
	Effort             string
	PermissionMode     string
	AppendSystemPrompt string
	ConfigDir          string
	ProfileCommand     string
	ProfileArgs        string
	AutoCompactWindow  string
	// ResumeAt is a pending rewind/fork point (chats.resume_at): with a resume
	// session the start adds `--resume-session-at <uuid> --fork-session`.
	ResumeAt string
}

// providerInstance is the subset of src/lib/providers.ts ProviderInstance a
// Claude start needs, as stored in config.json under "providers".
type providerInstance struct {
	OpenAI            *OpenAIConfig `json:"openai,omitempty"`
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	ProviderID        string        `json:"providerId"`
	Kind              string        `json:"kind"`
	Transport         string        `json:"transport"`
	Enabled           bool          `json:"enabled"`
	Binary            string        `json:"binary"`
	Args              []string      `json:"args"`
	ConfigDir         string        `json:"configDir"`
	AutoCompactWindow string        `json:"autoCompactWindow"`
	// Codex / ACP starts only.
	Env           map[string]string `json:"env"`
	TransportArgs []string          `json:"transportArgs"`
}

// catalogBinary is PROVIDER_CATALOG's default binary (src/lib/providers.ts
// binaryFor): an instance with no binary of its own runs its provider's.
var catalogBinary = map[string]string{
	"claude": "claude", "codex": "codex", "gemini": "gemini", "opencode": "opencode",
	"copilot": "copilot", "aider": "aider", "cursor": "cursor-agent",
}

// resolveRPCLaunch is the Codex / generic ACP start a chat row resolves to —
// AgentChat.vue's acpStartPayload, moved here so a start needs no view. The
// row's agent_kind names the provider instance (aliases included); the
// project's .burrow/config.toml supplies env_file and claude_config_dir.
// An unknown instance still starts Codex on its defaults (the phone creates
// rows as plain "codex"); a generic ACP adapter has nothing to guess from.
func resolveRPCLaunch(c Chat, transport string, providers []providerInstance, aliases map[string]string, cwd, projectConfigDir, envFile string) (AcpStartOpts, error) {
	opts := AcpStartOpts{
		ID: strconv.FormatInt(c.ID, 10), Cwd: cwd, ResumeSessionID: c.ClaudeSessionID,
		Env: map[string]string{}, EnvFile: envFile,
	}
	p := providerByIDOrName(c.AgentKind, providers, aliases)
	if p == nil {
		if transport != "codex-app-server" {
			return opts, fmt.Errorf("chat %d: no provider %q is configured for its ACP adapter", c.ID, c.AgentKind)
		}
		opts.Command, opts.Kind, opts.Args = "codex", "codex", []string{"app-server"}
		return opts, nil
	}
	for k, v := range p.Env {
		opts.Env[k] = v
	}
	if p.ProviderID == "openai-compatible" {
		if p.OpenAI == nil {
			return opts, fmt.Errorf("configure the endpoint and model in Settings > Providers")
		}
		cfg := *p.OpenAI
		opts.OpenAI = &cfg
		opts.SystemPrompt = c.AppendSystemPrompt
		return opts, nil
	}
	opts.Command = firstNonEmpty(strings.TrimSpace(p.Binary), catalogBinary[p.ProviderID], "npx")
	opts.Args = p.TransportArgs
	opts.Kind = firstNonEmpty(p.Kind, "custom")
	opts.ConfigDir = firstNonEmpty(projectConfigDir, p.Env["CLAUDE_CONFIG_DIR"])
	return opts, nil
}

// rpcLaunchFor reads resolveRPCLaunch's inputs fresh, like claudeLaunchFor.
func (a *App) rpcLaunchFor(c Chat, transport, cwd string) (AcpStartOpts, error) {
	var cfg struct {
		Providers []providerInstance `json:"providers"`
		Aliases   map[string]string  `json:"providerAliases"`
	}
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	opts, err := resolveRPCLaunch(c, transport, cfg.Providers, cfg.Aliases, cwd, projectClaudeConfigDir(cwd), projectSetting(cwd, "env_file"))
	// A side chat's first start forks its parent's thread (chatside.go).
	opts.Fork = opts.ResumeSessionID != "" && a.chatResumeAt(c.ID) == sideForkLatest
	return opts, err
}

// resolveClaudeLaunch is pure: the row's launch settings, its profile looked up
// in providers (aliases = config.json "providerAliases", legacy id → instance
// id), and the project's claude_config_dir as the config-dir fallback.
//
// Lookup order mirrors AgentChat.vue's selectedProfile: the row's profile by id
// (disabled instances included, like providers.byId), then by alias, else the
// first enabled Claude instance.
func resolveClaudeLaunch(c Chat, providers []providerInstance, aliases map[string]string, projectConfigDir string) ClaudeLaunch {
	out := ClaudeLaunch{
		Model:              c.Model,
		Effort:             c.Effort,
		PermissionMode:     c.PermissionMode,
		AppendSystemPrompt: c.AppendSystemPrompt,
		ConfigDir:          projectConfigDir,
	}
	p := findProfile(c.ProfileID, providers, aliases)
	if p == nil {
		return out
	}
	if p.ConfigDir != "" {
		out.ConfigDir = p.ConfigDir
	}
	out.ProfileCommand = p.Binary
	out.ProfileArgs = strings.Join(p.Args, " ")
	out.AutoCompactWindow = p.AutoCompactWindow
	return out
}

func findProfile(id string, providers []providerInstance, aliases map[string]string) *providerInstance {
	byID := func(id string) *providerInstance {
		for i := range providers {
			if providers[i].ID == id {
				return &providers[i]
			}
		}
		return nil
	}
	if id != "" {
		if p := byID(id); p != nil {
			return p
		}
		if alias := aliases[id]; alias != "" {
			if p := byID(alias); p != nil {
				return p
			}
		}
	}
	for i := range providers {
		if providers[i].Enabled && providers[i].ProviderID == "claude" {
			return &providers[i]
		}
	}
	return nil
}

// claudeLaunchFor reads the inputs resolveClaudeLaunch needs — config.json and
// <cwd>/.burrow/config.toml — fresh on every call, so an edited profile applies
// on the next start without a restart. A missing or broken file resolves as
// "nothing configured", never an error: a start must not fail on a hand-edit.
func (a *App) claudeLaunchFor(c Chat, cwd string) ClaudeLaunch {
	var cfg struct {
		Providers []providerInstance `json:"providers"`
		Aliases   map[string]string  `json:"providerAliases"`
	}
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return resolveClaudeLaunch(c, cfg.Providers, cfg.Aliases, projectClaudeConfigDir(cwd))
}

// projectClaudeConfigDir reads `claude_config_dir` from the [settings] table of
// <cwd>/.burrow/config.toml — the same file and key src/stores/scripts.ts
// parses for settingsFor(cwd). A line-level parse, like the frontend's: the
// file is written by serializeSettingsToml as `key = "value"`.
func projectClaudeConfigDir(cwd string) string {
	return projectSetting(cwd, "claude_config_dir")
}

func projectSetting(cwd, key string) string {
	if cwd == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(cwd, ".burrow", "config.toml"))
	if err != nil {
		return ""
	}
	return parseSettingsValue(string(b), key)
}

func parseSettingsValue(content, key string) string {
	inSettings := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inSettings = line == "[settings]"
			continue
		}
		if !inSettings {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return strings.Trim(v, `'`)
	}
	return ""
}

// backfillChatLaunchSettings copies the per-device launch settings AgentChat.vue
// kept in config.json onto the chat rows, once (plan decision 9). Only empty
// columns are filled, so it can never overwrite a row-owned value. The legacy
// keys are left in place: the frontend still reads them until PR2.
//
// Keys, as AgentChat.vue writes them (bare chat id; `modelKey`-scoped
// "<key>:<id>" entries belong to no current caller and are skipped):
// chatModelByChat, chatEffortByChat, chatPermissionMode.byChat,
// chatProfileSelection.
func (a *App) backfillChatLaunchSettings() {
	const marker = "launch_settings_backfill"
	if a.db == nil {
		return
	}
	var done int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM chat_migrations WHERE name = ?`, marker).Scan(&done); err != nil || done > 0 {
		return
	}
	raw, err := a.ReadConfig()
	if err != nil {
		return
	}
	var cfg struct {
		Model   map[string]string `json:"chatModelByChat"`
		Effort  map[string]string `json:"chatEffortByChat"`
		Profile map[string]string `json:"chatProfileSelection"`
		Perm    struct {
			ByChat map[string]string `json:"byChat"`
		} `json:"chatPermissionMode"`
	}
	// A broken hand-edited config.json backfills nothing but still sets the
	// marker below; the settings fall back to defaults, as they would anyway.
	_ = json.Unmarshal([]byte(raw), &cfg)

	tx, err := a.db.Begin()
	if err != nil {
		log.Printf("launch settings backfill: begin: %v", err)
		return
	}
	defer tx.Rollback()
	for _, f := range []struct {
		column string
		values map[string]string
	}{
		{"model", cfg.Model},
		{"effort", cfg.Effort},
		{"permission_mode", cfg.Perm.ByChat},
		{"profile_id", cfg.Profile},
	} {
		for key, v := range f.values {
			id, err := strconv.ParseInt(key, 10, 64)
			if err != nil || id <= 0 || v == "" {
				continue
			}
			// Column names come from the literal list above, never from input.
			if _, err := tx.Exec(`UPDATE chats SET `+f.column+` = ? WHERE id = ? AND `+f.column+` = ''`, v, id); err != nil {
				log.Printf("launch settings backfill: %s %d: %v", f.column, id, err)
				return
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO chat_migrations (name, done_at) VALUES (?, ?)`, marker, time.Now().UnixMilli()); err != nil {
		log.Printf("launch settings backfill: marker: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("launch settings backfill: commit: %v", err)
	}
}
