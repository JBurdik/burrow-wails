package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillByDir(list []SkillInfo, dir string) (SkillInfo, bool) {
	for _, s := range list {
		if s.Dir == dir {
			return s, true
		}
	}
	return SkillInfo{}, false
}

func TestSkillFrontmatter(t *testing.T) {
	root := t.TempDir()

	// A description with colons in it: cutting on the LAST colon, or splitting
	// on every one, is what would mangle this.
	writeSkill(t, filepath.Join(root, "burrow"), `---
name: burrow
description: Delegate work. Triggers: "run in parallel", "hand off".
allowed-tools: Bash(burrow:*)
---

# Burrow
`)
	// No frontmatter at all — the dir name has to carry the name.
	writeSkill(t, filepath.Join(root, "bare"), "# Bare skill\n")
	// Nested keys must not be read as top-level ones.
	writeSkill(t, filepath.Join(root, "nestedkeys"), `---
name: nestedkeys
metadata:
  description: not this one
---
`)

	got := collectSkills(root, "personal")
	if len(got) != 3 {
		t.Fatalf("want 3 skills, got %d (%+v)", len(got), got)
	}

	b, ok := skillByDir(got, "burrow")
	if !ok {
		t.Fatal("burrow missing")
	}
	if b.Name != "burrow" {
		t.Errorf("name = %q", b.Name)
	}
	if b.Description != `Delegate work. Triggers: "run in parallel", "hand off".` {
		t.Errorf("description = %q", b.Description)
	}
	if b.Source != "personal" || !b.Enabled {
		t.Errorf("source/enabled = %q/%v", b.Source, b.Enabled)
	}

	bare, _ := skillByDir(got, "bare")
	if bare.Name != "bare" || bare.Description != "" {
		t.Errorf("bare = %+v", bare)
	}

	nk, _ := skillByDir(got, "nestedkeys")
	if nk.Description != "" {
		t.Errorf("indented key leaked into the description: %q", nk.Description)
	}
}

// A plugin lays its skills out one level deeper. Without descending, a repo's
// own `.claude/skills/<plugin>/<skill>/SKILL.md` never reached the picker.
func TestCollectSkillsDescendsIntoGroups(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "gitnexus", "gitnexus-cli"), "---\nname: gitnexus-cli\ndescription: Run the CLI.\n---\n")
	writeSkill(t, filepath.Join(root, "gitnexus", "gitnexus-guide"), "---\nname: gitnexus-guide\ndescription: Tool reference.\n---\n")
	// A group dir that also holds a skill of its own is a leaf, not a group:
	// descending into it would report the same skill twice.
	writeSkill(t, filepath.Join(root, "solo"), "---\nname: solo\n---\n")
	writeSkill(t, filepath.Join(root, "solo", "inner"), "---\nname: inner\n---\n")

	got := collectSkills(root, "project")
	if _, ok := skillByDir(got, filepath.Join("gitnexus", "gitnexus-cli")); !ok {
		t.Errorf("nested skill missing from %+v", got)
	}
	if _, ok := skillByDir(got, "solo"); !ok {
		t.Error("leaf skill missing")
	}
	if _, ok := skillByDir(got, filepath.Join("solo", "inner")); ok {
		t.Error("descended into a dir that is itself a skill")
	}
	for _, s := range got {
		if s.Source != "project" {
			t.Errorf("%s source = %q", s.Dir, s.Source)
		}
	}
}

func TestCollectSkillsReportsDisabled(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "off")
	writeSkill(t, dir, "---\nname: off\ndescription: Turned off.\n---\n")
	// SetSkillEnabled(false) is exactly this rename.
	if err := os.Rename(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "SKILL.md.disabled")); err != nil {
		t.Fatal(err)
	}

	got := collectSkills(root, "personal")
	if len(got) != 1 || got[0].Enabled {
		t.Fatalf("want one disabled skill, got %+v", got)
	}
	// Still parsed, so Settings can show what a disabled skill would do.
	if got[0].Description != "Turned off." {
		t.Errorf("description = %q", got[0].Description)
	}
}

func TestSkillDescriptionIsTruncated(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "long"), "---\nname: long\ndescription: "+strings.Repeat("x", 2000)+"\n---\n")
	got := collectSkills(root, "personal")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if len([]rune(got[0].Description)) > skillDescMax+1 {
		t.Errorf("description not truncated: %d runes", len([]rune(got[0].Description)))
	}
	if !strings.HasSuffix(got[0].Description, "…") {
		t.Error("truncation is not marked")
	}
}

// A missing root is the normal case for a repo with no skills of its own.
func TestCollectSkillsMissingRoot(t *testing.T) {
	if got := collectSkills(filepath.Join(t.TempDir(), "nope"), "project"); got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}
