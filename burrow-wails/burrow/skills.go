package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SkillInfo mirrors list_skills' return shape — one entry per
// ~/.claude/skills/<dir>/SKILL.md, matching src-tauri/src/lib.rs.
type SkillInfo struct {
	Dir     string `json:"dir"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func claudeSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

func (a *App) ListSkills() ([]SkillInfo, error) {
	dir, err := claudeSkillsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []SkillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			disabledPath := skillPath + ".disabled"
			if _, err := os.Stat(disabledPath); err == nil {
				out = append(out, SkillInfo{Dir: e.Name(), Name: e.Name(), Enabled: false})
			}
			continue
		}
		out = append(out, SkillInfo{Dir: e.Name(), Name: e.Name(), Enabled: true})
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
