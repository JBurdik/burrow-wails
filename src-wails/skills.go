package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillInfo is one entry per `<root>/skills/**/SKILL.md`. `Description` comes
// from the file's YAML frontmatter — it used to be absent, which is why the
// composer's skill picker showed the placeholder "/name skill" for every row
// instead of what the skill actually does.
type SkillInfo struct {
	// Path of the skill dir relative to its root ("burrow", "gitnexus/gitnexus-cli").
	Dir         string `json:"dir"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// "personal" (~/.claude/skills) or "project" (<cwd>/.claude/skills).
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}

func claudeSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// Descriptions are shown truncated in one line anyway, and a skill's own can
// run to a couple of kilobytes (agent-browser's is ~1.2 KB of trigger phrases)
// — 44 of those is a payload nobody reads.
const skillDescMax = 300

// ponytail: single-line `key: value` frontmatter only. A folded block scalar
// (`description: >`) reads as empty rather than as garbage, which is the safe
// direction to be wrong in. Upgrade path: a real YAML parse if any skill needs it.
func skillFrontmatter(path string) (name, desc string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Descriptions are long; the default 64 KB token limit is not enough for a
	// pathological one and a scan error would silently drop the whole skill.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "" // no frontmatter block at all
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		// Only top-level keys: an indented line belongs to a nested mapping.
		if key != strings.TrimLeft(key, " \t") {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			desc = value
		}
	}
	if len(desc) > skillDescMax {
		desc = strings.TrimSpace(desc[:skillDescMax]) + "…"
	}
	return name, desc
}

// readSkillDir reports the skill living directly in `dir`, if any. A skill is
// enabled when SKILL.md is present and disabled when only SKILL.md.disabled is
// (that rename is how SetSkillEnabled toggles one).
func readSkillDir(dir, relDir, source string) (SkillInfo, bool) {
	skillPath := filepath.Join(dir, "SKILL.md")
	enabled := true
	if _, err := os.Stat(skillPath); err != nil {
		skillPath += ".disabled"
		if _, err := os.Stat(skillPath); err != nil {
			return SkillInfo{}, false
		}
		enabled = false
	}
	name, desc := skillFrontmatter(skillPath)
	if name == "" {
		name = filepath.Base(relDir)
	}
	return SkillInfo{Dir: relDir, Name: name, Description: desc, Source: source, Enabled: enabled}, true
}

// collectSkills walks one root. A directory that holds no SKILL.md of its own is
// treated as a *group* and descends one more level — which is how a plugin's
// skills are laid out (`.claude/skills/gitnexus/gitnexus-cli/SKILL.md`), and
// without it a repo's own skills were invisible to the picker.
func collectSkills(root, source string) []SkillInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil // missing root is not an error: most repos have no skills
	}
	out := []SkillInfo{}
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !isDir(dir) { // e.IsDir() doesn't follow symlinks; a symlinked skill dir needs Stat
			continue
		}
		if skill, ok := readSkillDir(dir, e.Name(), source); ok {
			out = append(out, skill)
			continue
		}
		subs, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			subDir := filepath.Join(dir, sub.Name())
			if !isDir(subDir) {
				continue
			}
			rel := filepath.Join(e.Name(), sub.Name())
			if skill, ok := readSkillDir(subDir, rel, source); ok {
				out = append(out, skill)
			}
		}
	}
	return out
}

// collectPluginSkills lists what installed Claude Code plugins ship — both
// `skills/*/SKILL.md` and `commands/*.md` — named the way the agent invokes
// them (`superpowers:brainstorming`). Read-only: they are not ours to toggle.
// ponytail: ignores enabledPlugins in settings.json, so a plugin that is
// installed but switched off still shows. Upgrade path: filter on it.
func collectPluginSkills() []SkillInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var reg struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &reg) != nil {
		return nil
	}
	keys := make([]string, 0, len(reg.Plugins))
	for k := range reg.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []SkillInfo{}
	seen := map[string]bool{}
	add := func(s SkillInfo) {
		if !seen[s.Name] {
			seen[s.Name] = true
			out = append(out, s)
		}
	}
	for _, key := range keys {
		plugin, _, _ := strings.Cut(key, "@")
		for _, inst := range reg.Plugins[key] {
			for _, s := range collectSkills(filepath.Join(inst.InstallPath, "skills"), "plugin") {
				s.Name = plugin + ":" + s.Name
				add(s)
			}
			cmds, _ := os.ReadDir(filepath.Join(inst.InstallPath, "commands"))
			for _, c := range cmds {
				base, ok := strings.CutSuffix(c.Name(), ".md")
				if !ok {
					continue
				}
				_, desc := skillFrontmatter(filepath.Join(inst.InstallPath, "commands", c.Name()))
				add(SkillInfo{Dir: "", Name: plugin + ":" + base, Description: desc, Source: "plugin", Enabled: true})
			}
		}
	}
	return out
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ListSkills reports the user's own skills plus, when `cwd` names a repo, that
// repo's checked-in ones, plus installed plugins' (only with a cwd, so the
// composer sees them and Settings — which can toggle/delete — does not). Settings passes "" — it manages personal skills only,
// which is what keeps SetSkillEnabled/DeleteSkill's `dir` unambiguous (they
// resolve it against ~/.claude/skills and must never be handed a project row).
func (a *App) ListSkills(cwd string) ([]SkillInfo, error) {
	dir, err := claudeSkillsDir()
	if err != nil {
		return nil, err
	}
	out := collectSkills(dir, "personal")
	if cwd != "" {
		out = append(out, collectPluginSkills()...)
		out = append(out, collectSkills(filepath.Join(cwd, ".claude", "skills"), "project")...)
	}
	return out, nil
}

func (a *App) SetSkillEnabled(dir string, enabled bool) error {
	skillsDir, err := claudeSkillsDir()
	if err != nil {
		return err
	}
	skillPath := filepath.Join(skillsDir, dir, "SKILL.md")
	disabledPath := skillPath + ".disabled"
	if enabled {
		return os.Rename(disabledPath, skillPath)
	}
	return os.Rename(skillPath, disabledPath)
}

func (a *App) DeleteSkill(dir string) error {
	skillsDir, err := claudeSkillsDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(skillsDir, dir))
}

// --- MCP servers (~/.claude.json's mcpServers block, matching
// list_mcp_servers/add_mcp_server/remove_mcp_server) ---

func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

func readClaudeConfig() (map[string]any, error) {
	path, err := claudeConfigPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func writeClaudeConfig(cfg map[string]any) error {
	path, err := claudeConfigPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a *App) ListMcpServers() (map[string]any, error) {
	cfg, err := readClaudeConfig()
	if err != nil {
		return nil, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	return servers, nil
}

func (a *App) AddMcpServer(name string, config map[string]any) error {
	cfg, err := readClaudeConfig()
	if err != nil {
		return err
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
	}
	servers[name] = config
	cfg["mcpServers"] = servers
	return writeClaudeConfig(cfg)
}

func (a *App) RemoveMcpServer(name string) error {
	cfg, err := readClaudeConfig()
	if err != nil {
		return err
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}
	delete(servers, name)
	cfg["mcpServers"] = servers
	return writeClaudeConfig(cfg)
}
