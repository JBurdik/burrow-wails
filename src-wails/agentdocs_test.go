package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The marker block must be replaceable in place, never duplicated, and must
// leave the user's own CLAUDE.md content untouched.
func TestMergeDocBlockReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My rules\n\nAlways use tabs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mergeDocBlock(path, "first version")
	mergeDocBlock(path, "second version")

	got := read(t, path)
	if !strings.HasPrefix(got, "# My rules\n\nAlways use tabs.") {
		t.Errorf("user content mangled:\n%s", got)
	}
	if n := strings.Count(got, docBeginMarker); n != 1 {
		t.Errorf("want exactly one block, got %d:\n%s", n, got)
	}
	if strings.Contains(got, "first version") || !strings.Contains(got, "second version") {
		t.Errorf("block not replaced:\n%s", got)
	}
}

func TestMergeDocBlockCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	mergeDocBlock(path, "hello")
	if got := read(t, path); got != docBeginMarker+"\nhello\n"+docEndMarker+"\n" {
		t.Errorf("unexpected content: %q", got)
	}
}

// Copilot needs its skills dir registered, once, alongside whatever else the
// user has in settings.json.
func TestRegisterCopilotSkillDirIsAdditive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"skillDirectories":["/existing"],"model":"gpt"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registerCopilotSkillDir(path, "/new/skills")
	registerCopilotSkillDir(path, "/new/skills")

	root := readJSON(t, path)
	if root["model"] != "gpt" {
		t.Errorf("unrelated setting lost: %v", root)
	}
	dirs := root["skillDirectories"].([]any)
	if len(dirs) != 2 || dirs[0] != "/existing" || dirs[1] != "/new/skills" {
		t.Errorf("want [/existing /new/skills], got %v", dirs)
	}
}

// The embedded skill has to survive the go:embed path, and the docs the CLAUDE.md
// rule points at have to exist.
func TestEmbeddedDocsArePresent(t *testing.T) {
	dir := t.TempDir()
	writeSkills(dir)
	body := read(t, filepath.Join(dir, "skills", "burrow", "SKILL.md"))
	if !strings.HasPrefix(body, "---\nname: burrow\n") {
		t.Errorf("skill frontmatter missing:\n%s", body[:min(80, len(body))])
	}
	for _, asset := range []string{"agentdocs/claude-rule.md", "agentdocs/codex-agents.md"} {
		if docAsset(asset) == "" {
			t.Errorf("%s empty or missing", asset)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
