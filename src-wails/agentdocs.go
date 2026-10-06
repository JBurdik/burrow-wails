package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Agent docs: teaching each agent CLI that `burrow spawn` exists.
//
// Ported from install_agent_docs in src-tauri/src/lib.rs, but the doc bodies
// live as real markdown under agentdocs/ instead of Go string constants —
// they're prose, and prose in a string literal never gets proofread. Claude and
// Copilot read a skill (lazily loaded, so the always-in-context CLAUDE.md rule
// is what stops them reaching for their built-in Agent tool first); Codex has
// no skill mechanism, so it gets the same content as an AGENTS.md block.
//
// The skill dir is ours, so it's written wholesale. CLAUDE.md/AGENTS.md are the
// user's, so we only own a marker-delimited block inside them.
//
// Claude no longer gets the CLAUDE.md block: it was loaded by every Claude
// session on the machine, Burrow or not. The rule now rides in only where
// Burrow launches Claude — `--append-system-prompt` for chats, the burrow mod
// (agentdocs/mod/burrow, via CLAUDE_CODE_PLUGIN_DIRS) for PTY tabs.
//
// `all:` because the mod's manifest lives in a dot-dir (.claude-plugin).
//
//go:embed all:agentdocs
var agentDocs embed.FS

const (
	docBeginMarker = "<!-- BURROW:BEGIN -->"
	docEndMarker   = "<!-- BURROW:END -->"
)

// InstallAgentDocs re-runs the agent-docs install on demand (Settings /
// command palette), so a user can pick up a skill update without restarting
// the app.
func (a *App) InstallAgentDocs() {
	installAgentDocs()
}

func installAgentDocs() {
	claude, codex, copilot := hookDirs()

	for _, dir := range claude {
		writeSkills(dir)
		removeDocBlock(filepath.Join(dir, "CLAUDE.md"))
	}
	for _, dir := range codex {
		mergeDocBlock(filepath.Join(dir, "AGENTS.md"), docAsset("agentdocs/codex-agents.md"))
	}
	// Copilot reads the same SKILL.md spec as Claude, but only from dirs listed
	// in `skillDirectories` — there's no implicit skills/ lookup — so the write
	// has to be followed by a registration.
	for _, dir := range copilot {
		writeSkills(dir)
		registerCopilotSkillDir(filepath.Join(dir, "settings.json"), filepath.Join(dir, "skills"))
	}
}

func docAsset(path string) string {
	b, err := agentDocs.ReadFile(path)
	if err != nil {
		log.Printf("agent docs: missing embedded %s: %v", path, err)
		return ""
	}
	return strings.TrimSpace(string(b))
}

// writeSkills copies every embedded skill into <dir>/skills/<name>/SKILL.md.
func writeSkills(dir string) {
	entries, err := agentDocs.ReadDir("agentdocs/skills")
	if err != nil {
		return
	}
	for _, e := range entries {
		body, err := agentDocs.ReadFile("agentdocs/skills/" + e.Name() + "/SKILL.md")
		if err != nil {
			continue
		}
		target := filepath.Join(dir, "skills", e.Name())
		if err := os.MkdirAll(target, 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(target, "SKILL.md"), body, 0o644); err != nil {
			log.Printf("agent docs: write %s: %v", target, err)
		}
	}
}

// mergeDocBlock puts `body` inside our marker block in a markdown file the user
// also writes to: replacing an existing block in place, else appending one.
// Everything outside the markers is left byte-for-byte alone.
func mergeDocBlock(path, body string) {
	if body == "" {
		return
	}
	block := docBeginMarker + "\n" + body + "\n" + docEndMarker
	existing, _ := os.ReadFile(path)
	merged := replaceDocBlock(string(existing), block)
	if merged == string(existing) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, []byte(merged), 0o644); err != nil {
		log.Printf("agent docs: write %s: %v", path, err)
	}
}

// removeDocBlock drops our marker block (and the blank line we put before it)
// from a file the user also writes to. No block → the file is not touched.
func removeDocBlock(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	existing := string(b)
	start := strings.Index(existing, docBeginMarker)
	end := strings.Index(existing, docEndMarker)
	if start < 0 || end < start {
		return
	}
	head := strings.TrimRight(existing[:start], "\n")
	tail := strings.TrimLeft(existing[end+len(docEndMarker):], "\n")
	out := head
	if head != "" && tail != "" {
		out += "\n\n"
	}
	out += tail
	if head != "" && tail == "" {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		log.Printf("agent docs: write %s: %v", path, err)
	}
}

// claudeModDir is where installClaudeMod lays the burrow Claude Code mod.
func claudeModDir(dataDir string) string {
	return filepath.Join(dataDir, "claude-mod", "burrow")
}

// installClaudeMod writes the embedded mod wholesale (the dir is ours). Its
// tests stay behind: they're for `claude plugin test` in the repo.
func installClaudeMod(dataDir string) {
	const root = "agentdocs/mod/burrow"
	target := claudeModDir(dataDir)
	_ = os.RemoveAll(target)
	err := fs.WalkDir(agentDocs, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
		if d.IsDir() {
			if rel == "tests" {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0o755)
		}
		body, err := agentDocs.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, rel), body, 0o644)
	})
	if err != nil {
		log.Printf("claude mod: install %s: %v", target, err)
	}
}

func replaceDocBlock(existing, block string) string {
	start := strings.Index(existing, docBeginMarker)
	end := strings.Index(existing, docEndMarker)
	switch {
	case start >= 0 && end > start:
		return existing[:start] + block + existing[end+len(docEndMarker):]
	case strings.TrimSpace(existing) == "":
		return block + "\n"
	default:
		return strings.TrimRight(existing, "\n") + "\n\n" + block + "\n"
	}
}

// registerCopilotSkillDir adds `dir` to skillDirectories in Copilot's
// settings.json without touching the user's other settings. Unparseable file →
// skip, same rule as the hook merge: never destroy what we can't read.
func registerCopilotSkillDir(path, dir string) {
	existing, _ := os.ReadFile(path)
	root := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			log.Printf("agent docs: skipping unparseable %s: %v", path, err)
			return
		}
	}
	arr, _ := root["skillDirectories"].([]any)
	for _, v := range arr {
		if s, _ := v.(string); s == dir {
			return // already registered
		}
	}
	root["skillDirectories"] = append(arr, dir)
	writeJSONFile(path, root)
}
