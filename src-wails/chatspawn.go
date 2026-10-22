package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"burrow/internal/control"
)

// spawn's chat half, in Go (docs/plans/006-chat-runtime.md, decision 10):
// create the sub-agent's row with its launch settings and send it the task
// through the chat runtime. It used to be a UI action — the row was created by
// the frontend and the process started when SubAgentHost.vue mounted a hidden
// AgentChat for it, so a spawn needed a live desktop. Every transport runs
// here now (Claude, Codex, generic ACP). Ported from
// controlBridge.ts's spawn(); the resolution order is the same, plus decision
// 10's "else the parent's" for the launch settings.

// subagentProfile mirrors src/lib/subagentProfiles.ts SubagentProfile.
type subagentProfile struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Instructions   string `json:"instructions"`
	AgentID        string `json:"agentId"`
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"`
}

// defaultSubagentProfiles is defaultSubagentProfiles() in
// src/lib/subagentProfiles.ts — TestSubagentProfileDefaultsMatchFrontend
// keeps the two copies of the instructions in step.
func defaultSubagentProfiles() []subagentProfile {
	return []subagentProfile{
		{ID: "scout", Name: "Scout", PermissionMode: "plan", Instructions: "Investigate the task and locate relevant code. Do not change files. Report concrete findings, file paths, and a suggested approach to the parent agent."},
		{ID: "worker", Name: "Worker", PermissionMode: "default", Instructions: "Implement the assigned task within its stated scope. Respect repository instructions and other agents' work. Run appropriate checks and report the changes, validation, and any remaining issues."},
		{ID: "reviewer", Name: "Reviewer", PermissionMode: "plan", Instructions: "Review the assigned changes for bugs, regressions, and missing validation. Do not change files. Report actionable findings ordered by severity, with file paths, evidence, and suggested fixes. Say explicitly when no findings remain."},
	}
}

// findSubagentProfile is normalizeSubagentProfiles + find: a saved row
// overrides a default's instructions/agent/model/permission, nothing else.
func findSubagentProfile(id string, saved []map[string]any) (subagentProfile, bool) {
	for _, p := range defaultSubagentProfiles() {
		if p.ID != id {
			continue
		}
		for _, row := range saved {
			if row["id"] != id {
				continue
			}
			if v, ok := row["instructions"].(string); ok {
				p.Instructions = v
			}
			p.AgentID, _ = row["agentId"].(string)
			p.Model, _ = row["model"].(string)
			if v, _ := row["permissionMode"].(string); oneOf(v, "default", "auto", "plan") {
				p.PermissionMode = v
			}
		}
		return p, true
	}
	return subagentProfile{}, false
}

func profileTask(p subagentProfile, task string) string {
	return "Role: " + p.Name + "\n\n" + strings.TrimSpace(p.Instructions) + "\n\nTask:\n" + strings.TrimSpace(task)
}

// providerByIDOrName is providers.byId (aliases included) then a
// case-insensitive name match, as controlBridge.ts's spawn did.
func providerByIDOrName(want string, providers []providerInstance, aliases map[string]string) *providerInstance {
	for _, id := range []string{want, aliases[want]} {
		for i := range providers {
			if id != "" && providers[i].ID == id {
				return &providers[i]
			}
		}
	}
	for i := range providers {
		if strings.EqualFold(providers[i].Name, want) {
			return &providers[i]
		}
	}
	return nil
}

// chatTransportOf is chatTransportFor() in src/lib/providers.ts.
func chatTransportOf(p providerInstance) string {
	if p.Transport != "" && p.Transport != "none" {
		return p.Transport
	}
	if p.Kind == "claude" {
		return "claude-cli"
	}
	return "acp"
}

type spawnConfig struct {
	Providers []providerInstance `json:"providers"`
	Aliases   map[string]string  `json:"providerAliases"`
	Profiles  []map[string]any   `json:"subagentProfiles"`
	UI        struct {
		DefaultChatAgent string `json:"defaultChatAgent"`
	} `json:"uiPrefs"`
}

// chatRuntimeAdapter is the App behind control's ChatRuntime — unexported
// methods, so they don't become Wails bindings / remote commands.
type chatRuntimeAdapter struct{ a *App }

func (r chatRuntimeAdapter) SpawnSubagent(req control.SpawnRequest) (control.SpawnedChat, error) {
	return r.a.spawnSubagent(req)
}

func (r chatRuntimeAdapter) SendToChat(chatID int64, text string) error {
	return r.a.ChatSend(chatID, text, nil)
}

func (a *App) spawnSubagent(req control.SpawnRequest) (control.SpawnedChat, error) {
	var none control.SpawnedChat
	if strings.TrimSpace(req.Task) == "" {
		return none, fmt.Errorf("spawn needs a task")
	}
	parent, err := a.chatByID(req.ParentChatID)
	if err != nil {
		return none, err
	}
	var cfg spawnConfig
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}

	task := req.Task
	var profile *subagentProfile
	if id := strings.ToLower(req.Profile); id != "" {
		p, ok := findSubagentProfile(id, cfg.Profiles)
		if !ok {
			return none, fmt.Errorf("Unknown sub-agent profile %q. Use scout, worker, or reviewer.", id)
		}
		profile, task = &p, profileTask(p, req.Task)
	}

	// Agent: the caller's, else the profile's, else the parent's own, else the
	// user's default chat agent, else the first enabled one.
	wanted := req.Agent
	if wanted == "" && profile != nil {
		wanted = profile.AgentID
	}
	var inst *providerInstance
	if wanted != "" {
		if inst = providerByIDOrName(wanted, cfg.Providers, cfg.Aliases); inst == nil {
			return none, fmt.Errorf("no agent named %q — call list_agents to see the configured ones", wanted)
		}
	}
	if inst == nil && parent.AgentKind != "" {
		inst = providerByIDOrName(parent.AgentKind, cfg.Providers, cfg.Aliases)
	}
	if inst == nil {
		def := cfg.UI.DefaultChatAgent
		if def == "" {
			def = "claude"
		}
		inst = providerByIDOrName(def, cfg.Providers, cfg.Aliases)
	}
	if inst == nil {
		for i := range cfg.Providers {
			if cfg.Providers[i].Enabled {
				inst = &cfg.Providers[i]
				break
			}
		}
	}
	if inst == nil {
		return none, fmt.Errorf("no agent is configured in Settings > Providers")
	}
	if inst.Transport == "none" && inst.Kind != "claude" {
		return none, fmt.Errorf("This provider does not offer an embedded chat.")
	}
	transport := chatTransportOf(*inst)
	if profile != nil && (!inst.Enabled || (!oneOf(transport, "claude-cli", "codex-app-server") && inst.ProviderID != "openai-compatible")) {
		return none, fmt.Errorf("Sub-agent profiles require an enabled Claude CLI, Codex app-server, or OpenAI Compatible provider. Choose one in Settings > Sub-agent profiles.")
	}
	// The parent's launch settings only mean something to the same runtime: a
	// Claude model id is no Codex model, a Codex mode no Claude one.
	var inherit Chat
	if chatTransport(parent) == transport && (inst.ProviderID != "openai-compatible" || parent.AgentKind == inst.ID) {
		inherit = parent
	}

	// Workspace: the one the caller's cwd names (a worktree spawn nests under
	// the worktree), else the parent's.
	wsID := parent.WorkspaceID
	if req.Cwd != "" && a.db != nil {
		var id int64
		if a.db.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, req.Cwd).Scan(&id) == nil {
			wsID = id
		}
	}

	row := Chat{
		WorkspaceID:    wsID,
		ParentChatID:   parent.ID,
		Title:          subagentTitle(firstNonEmpty(strings.TrimSpace(req.Name), req.Task)),
		AgentKind:      inst.ID,
		Transport:      transport,
		Model:          firstNonEmpty(req.Model, profileField(profile, func(p *subagentProfile) string { return p.Model }), inherit.Model),
		Effort:         inherit.Effort,
		PermissionMode: firstNonEmpty(profileField(profile, func(p *subagentProfile) string { return p.PermissionMode }), inherit.PermissionMode),
		ProfileID:      inst.ID,
		LastActivityAt: time.Now().UnixMilli(),
	}
	c, err := a.CreateChat(row)
	if err != nil {
		return none, err
	}
	if err := a.ChatSend(c.ID, task, nil); err != nil {
		// A row whose process never started is a sub-agent nobody can reach.
		_ = a.DeleteChat(c.ID)
		return none, err
	}
	name := inst.Name
	if name == "" {
		name = inst.ID
	}
	a.publishSubagentRow(parent.ID, c.ID, task, name)
	return control.SpawnedChat{ChatID: c.ID, WorkspaceID: wsID, Task: task, AgentName: name}, nil
}

// SpawnSubagentChat is the same spawn for an in-app caller (the Right Panel's
// spawn button goes through controlBridge's spawn()).
func (a *App) SpawnSubagentChat(parentChatID int64, task, profile, agent, model, cwd string) (map[string]any, error) {
	got, err := a.spawnSubagent(control.SpawnRequest{
		Task: task, Profile: profile, Agent: agent, Model: model, Cwd: cwd, ParentChatID: parentChatID,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"chat_id": got.ChatID, "workspace_id": got.WorkspaceID,
		"task": got.Task, "agent_name": got.AgentName}, nil
}

func profileField(p *subagentProfile, f func(*subagentProfile) string) string {
	if p == nil {
		return ""
	}
	return f(p)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// subagentTitle: the task's first line, cut to the 60 runes a chat title gets.
func subagentTitle(task string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if r := []rune(line); len(r) > 60 {
		line = string(r[:60])
	}
	return line
}

// publishSubagentRow records "spawned a sub-agent" in the PARENT's transcript
// through the parent's own chat_stream (a chat-note row), so every client sees
// it live, a restart replays it, and no client has to read-modify-write the
// parent's chat_messages — or even be running.
func (a *App) publishSubagentRow(parentChatID, childChatID int64, task, agentName string) {
	a.emitChatLine(strconv.FormatInt(parentChatID, 10), chatNoteKind, subagentNote(childChatID, task, agentName))
}

// subagentNote is the chat-note line for that row: the task's first line as
// the label, and the child it opens.
func subagentNote(childChatID int64, task, agentName string) string {
	text := "Spawned Sub-agent"
	if label := strings.TrimSpace(strings.SplitN(task, "\n", 2)[0]); label != "" {
		text = "Spawned sub-agent: " + label
	}
	note, _ := json.Marshal(chatNoteEnvelope{
		Form: "row", Role: "system-info", Text: text,
		SubagentChatID: childChatID, SubagentAgent: agentName,
	})
	return string(note)
}
