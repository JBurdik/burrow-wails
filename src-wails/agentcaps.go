package main

import (
	"encoding/json"
	"strings"
)

// list_agents in Go: what an agent can delegate to, read from config.json with
// no window open (spawn already runs here; asking the UI meant an agent could
// spawn but not see on what). Modelled on t3code's orchestrator_capabilities:
// availability with a reason the model can act on, a short model list, the
// modes each runtime takes, and what a sub-agent inherits from its caller.

// capsModel is one model a runtime takes.
type capsModel struct {
	ID            string   `json:"id"`
	Label         string   `json:"label,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
}

type capsAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Provider  string `json:"provider,omitempty"`
	Transport string `json:"transport"`
	// Chat: can run as a chat sub-agent. false = terminal tab only.
	Chat      bool   `json:"chat"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Default: the agent spawn picks when given no agent (and no profile agent).
	Default bool `json:"default,omitempty"`
	// Profiles: takes --profile scout|worker|reviewer.
	Profiles bool `json:"profiles"`
	// InheritsCaller: same runtime as the caller, so a sub-agent here starts
	// with the caller's model / effort / permission mode unless overridden.
	InheritsCaller  bool        `json:"inherits_caller,omitempty"`
	Models          []capsModel `json:"models,omitempty"`
	OmittedModels   int         `json:"omitted_models,omitempty"`
	ModelsNote      string      `json:"models_note,omitempty"`
	PermissionModes []string    `json:"permission_modes,omitempty"`
}

type capsCaller struct {
	ChatID         int64  `json:"chat_id"`
	Agent          string `json:"agent"`
	Transport      string `json:"transport"`
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode"`
	IsSubagent     bool   `json:"is_subagent,omitempty"`
}

type capsProfile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Agent          string `json:"agent,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode"`
	Instructions   string `json:"instructions"`
}

type agentCaps struct {
	Caller   *capsCaller   `json:"caller,omitempty"`
	Agents   []capsAgent   `json:"agents"`
	Profiles []capsProfile `json:"profiles"`
	Rules    []string      `json:"rules"`
}

// claudeModels is MODELS_BY_AGENT.claude in src/lib/chatModels.ts — the
// Claude CLI has no discovery call. TestClaudeModelsMatchFrontend keeps the
// ids in step.
var (
	opusEfforts   = []string{"low", "medium", "high", "xhigh", "max"}
	sonnetEfforts = []string{"low", "medium", "high", "max"}
	claudeModels  = []capsModel{
		{"claude-opus-5-5", "Claude Opus 5.5", opusEfforts, "medium"},
		{"claude-fable-5-1", "Claude Fable 5.1", opusEfforts, "high"},
		{"claude-opus-5", "Claude Opus 5", opusEfforts, "high"},
		{"claude-sonnet-5-5", "Claude Sonnet 5.5", sonnetEfforts, "high"},
		{"claude-sonnet-5", "Claude Sonnet 5", sonnetEfforts, "high"},
		{"claude-haiku-5-5", "Claude Haiku 5.5", opusEfforts, "medium"},
		{"claude-fable-5", "Claude Fable 5", opusEfforts, "high"},
		{"claude-opus-4-8", "Claude Opus 4.8", opusEfforts, "high"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6", sonnetEfforts, "high"},
		{"claude-haiku-4-5-20251001", "Claude Haiku 4.5", nil, ""},
	}
)

// shortModelList caps what lands in the caller's context per agent; the
// `agent` argument returns the rest (a runtime like OpenCode reports hundreds).
const shortModelList = 8

// permissionModesFor is the mode vocabulary each runtime accepts as
// spawn --permission-mode (Claude's --permission-mode; Codex = codexModes).
func permissionModesFor(transport string) []string {
	switch transport {
	case "claude-cli":
		return []string{"plan", "default", "acceptEdits", "auto", "dontAsk", "bypassPermissions"}
	case "codex-app-server":
		return []string{"plan", "supervised", "auto", "full-access"}
	}
	return nil // generic ACP: the adapter reports its own modes per session
}

var capsRules = []string{
	"A sub-agent's permission_mode may narrow the caller's, never widen it (plan < default < acceptEdits/auto < dontAsk < full-access).",
	"Model, effort and permission mode are inherited only by an agent with inherits_caller; elsewhere they start at that runtime's defaults unless passed.",
	"A sub-agent cannot spawn sub-agents; it gets its task prompt (plus a profile's instructions), not your conversation.",
	"Wait with wait_result --chat-id or collect_results; a finished sub-agent's result is also delivered to you when your turn settles.",
}

func (r chatRuntimeAdapter) ListAgents(caller int64, agent string) (any, error) {
	return r.a.listAgents(caller, agent)
}

func (r chatRuntimeAdapter) ListSubagentProfiles() (any, error) {
	var cfg spawnConfig
	if raw, err := r.a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return subagentProfiles(cfg), nil
}

func (a *App) listAgents(callerID int64, only string) (agentCaps, error) {
	var cfg capsConfig
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	var caller *Chat
	if callerID > 0 {
		if c, err := a.chatByID(callerID); err == nil {
			caller = &c
		}
	}
	return buildAgentCaps(cfg, caller, only, resolveAgentBin), nil
}

type capsConfig struct {
	spawnConfig
	Seen map[string][]capsModel `json:"chatModelsSeen"`
}

// UnmarshalJSON: chatModelsSeen is the frontend's ModelEntry
// (defaultEffort, camelCase); everything else is spawnConfig's.
func (c *capsConfig) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &c.spawnConfig); err != nil {
		return err
	}
	var raw struct {
		Seen map[string][]struct {
			ID            string   `json:"id"`
			Label         string   `json:"label"`
			Efforts       []string `json:"efforts"`
			DefaultEffort string   `json:"defaultEffort"`
		} `json:"chatModelsSeen"`
	}
	_ = json.Unmarshal(b, &raw)
	c.Seen = map[string][]capsModel{}
	for k, list := range raw.Seen {
		for _, m := range list {
			c.Seen[k] = append(c.Seen[k], capsModel{m.ID, m.Label, m.Efforts, m.DefaultEffort})
		}
	}
	return nil
}

// buildAgentCaps is pure: config + caller in, the report out. lookBin is
// resolveAgentBin (a test passes a fake).
func buildAgentCaps(cfg capsConfig, caller *Chat, only string, lookBin func(name, root string) string) agentCaps {
	out := agentCaps{Agents: []capsAgent{}, Profiles: subagentProfiles(cfg.spawnConfig), Rules: capsRules}
	parentKind := ""
	if caller != nil {
		parentKind = caller.AgentKind
		out.Caller = &capsCaller{
			ChatID: caller.ID, Agent: caller.AgentKind, Transport: chatTransport(*caller),
			Model: caller.Model, Effort: caller.Effort,
			PermissionMode: firstNonEmpty(caller.PermissionMode, "default"),
			IsSubagent:     caller.ParentChatID > 0,
		}
	}
	defID := ""
	if def, err := pickSpawnAgent(cfg.spawnConfig, "", parentKind); err == nil {
		defID = def.ID
	}
	var want *providerInstance
	if only != "" {
		want = providerByIDOrName(only, cfg.Providers, cfg.Aliases)
	}
	for _, p := range cfg.Providers {
		if only != "" && (want == nil || p.ID != want.ID) {
			continue
		}
		out.Agents = append(out.Agents, describeAgent(p, cfg, caller, p.ID == defID, only != "", lookBin))
	}
	return out
}

func describeAgent(p providerInstance, cfg capsConfig, caller *Chat, isDefault, full bool, lookBin func(name, root string) string) capsAgent {
	transport := chatTransportOf(p)
	chat := p.Transport != "none" || p.Kind == "claude"
	ag := capsAgent{
		ID: p.ID, Name: firstNonEmpty(p.Name, p.ID), Kind: p.Kind, Provider: p.ProviderID,
		Transport: transport, Chat: chat, Default: isDefault, Profiles: chat && runsProfiles(p),
		PermissionModes: permissionModesFor(transport),
	}
	if !chat {
		ag.Transport = "tab"
	}
	ag.Available, ag.Reason = agentAvailable(p, chat, lookBin)
	if caller != nil && chat && chatTransport(*caller) == transport &&
		(p.ProviderID != "openai-compatible" || caller.AgentKind == p.ID) {
		ag.InheritsCaller = true
	}

	models := cfg.Seen[p.ID]
	switch {
	case p.Kind == "claude" || transport == "claude-cli":
		models = claudeModels
	case p.ProviderID == "openai-compatible" && p.OpenAI != nil && p.OpenAI.Model != "" && len(models) == 0:
		models = []capsModel{{ID: p.OpenAI.Model}}
	}
	if len(models) == 0 && chat {
		ag.ModelsNote = "no model list yet — this runtime reports its models once a chat with it has started; omit model to use its default"
	}
	// The caller's own model first: "same model as me" is the common pick.
	if caller != nil && ag.InheritsCaller && caller.Model != "" {
		models = withFirst(models, caller.Model)
	}
	if !full && len(models) > shortModelList {
		ag.OmittedModels = len(models) - shortModelList
		models = models[:shortModelList]
	}
	ag.Models = models
	return ag
}

// agentAvailable: can this instance start right now, and if not, why — in
// words the calling model can relay or act on.
func agentAvailable(p providerInstance, chat bool, lookBin func(name, root string) string) (bool, string) {
	if !p.Enabled {
		return false, "disabled in Settings > Providers"
	}
	if p.ProviderID == "openai-compatible" {
		if p.OpenAI == nil {
			return false, "endpoint and model not configured in Settings > Providers"
		}
		return true, ""
	}
	bin := strings.TrimSpace(p.Binary)
	if bin == "" {
		if chat && chatTransportOf(p) != "claude-cli" {
			bin = catalogChatBinary[p.ProviderID]
		}
		bin = firstNonEmpty(bin, catalogBinary[p.ProviderID], catalogBinary[p.Kind])
	}
	if bin == "" || bin == "npx" {
		return true, "" // an npx adapter installs on first start
	}
	if lookBin(bin, "") == "" {
		return false, bin + " not found (install it or set its path in Settings > Providers)"
	}
	return true, ""
}

func withFirst(models []capsModel, id string) []capsModel {
	out := []capsModel{}
	for _, m := range models {
		if m.ID == id {
			out = append([]capsModel{m}, out...)
		} else {
			out = append(out, m)
		}
	}
	if len(out) == 0 || out[0].ID != id {
		out = append([]capsModel{{ID: id}}, out...)
	}
	return out
}

func subagentProfiles(cfg spawnConfig) []capsProfile {
	out := []capsProfile{}
	for _, d := range defaultSubagentProfiles() {
		p, _ := findSubagentProfile(d.ID, cfg.Profiles)
		out = append(out, capsProfile{p.ID, p.Name, p.AgentID, p.Model, p.PermissionMode, p.Instructions})
	}
	return out
}
