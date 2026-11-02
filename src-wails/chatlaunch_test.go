package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestResolveClaudeLaunch(t *testing.T) {
	work := providerInstance{ID: "claude-work", ProviderID: "claude", Enabled: true,
		Binary: "/opt/claude", Args: []string{"--a", "--b"}, ConfigDir: "/cfg/work", AutoCompactWindow: "200000"}
	plain := providerInstance{ID: "claude", ProviderID: "claude", Enabled: true}
	codex := providerInstance{ID: "codex", ProviderID: "codex", Enabled: true, ConfigDir: "/cfg/codex"}
	off := providerInstance{ID: "claude-off", ProviderID: "claude", Enabled: false, ConfigDir: "/cfg/off"}
	row := Chat{Model: "opus", Effort: "high", PermissionMode: "plan", AppendSystemPrompt: "be brief"}

	tests := []struct {
		name      string
		chat      Chat
		providers []providerInstance
		aliases   map[string]string
		project   string
		want      ClaudeLaunch
	}{
		{
			name:      "profile found",
			chat:      withProfile(row, "claude-work"),
			providers: []providerInstance{plain, work},
			project:   "/cfg/project",
			want: ClaudeLaunch{Model: "opus", Effort: "high", PermissionMode: "plan", AppendSystemPrompt: "be brief",
				ConfigDir: "/cfg/work", ProfileCommand: "/opt/claude", ProfileArgs: "--a --b", AutoCompactWindow: "200000"},
		},
		{
			name:      "profile via legacy alias",
			chat:      withProfile(Chat{}, "old-id"),
			providers: []providerInstance{plain, work},
			aliases:   map[string]string{"old-id": "claude-work"},
			want: ClaudeLaunch{ConfigDir: "/cfg/work", ProfileCommand: "/opt/claude", ProfileArgs: "--a --b",
				AutoCompactWindow: "200000"},
		},
		{
			name:      "disabled profile still resolves by id",
			chat:      withProfile(Chat{}, "claude-off"),
			providers: []providerInstance{plain, off},
			want:      ClaudeLaunch{ConfigDir: "/cfg/off"},
		},
		{
			name:      "profile missing falls back to first enabled claude instance",
			chat:      withProfile(row, "gone"),
			providers: []providerInstance{codex, off, work},
			want: ClaudeLaunch{Model: "opus", Effort: "high", PermissionMode: "plan", AppendSystemPrompt: "be brief",
				ConfigDir: "/cfg/work", ProfileCommand: "/opt/claude", ProfileArgs: "--a --b", AutoCompactWindow: "200000"},
		},
		{
			name:      "profile without config dir falls back to project",
			chat:      withProfile(Chat{}, "claude"),
			providers: []providerInstance{plain, work},
			project:   "/cfg/project",
			want:      ClaudeLaunch{ConfigDir: "/cfg/project"},
		},
		{
			name:    "empty row, no providers",
			project: "/cfg/project",
			want:    ClaudeLaunch{ConfigDir: "/cfg/project"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveClaudeLaunch(tt.chat, tt.providers, tt.aliases, tt.project); got != tt.want {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func withProfile(c Chat, id string) Chat {
	c.ProfileID = id
	return c
}

func TestProjectClaudeConfigDir(t *testing.T) {
	dir := t.TempDir()
	if got := projectClaudeConfigDir(dir); got != "" {
		t.Fatalf("no file: got %q", got)
	}
	os.MkdirAll(filepath.Join(dir, ".burrow"), 0o755)
	toml := "[settings]\nenv_file = \".env\"\nclaude_config_dir = \"/Users/x/.claude-work\"\n\n[[scripts]]\nclaude_config_dir = \"wrong\"\n"
	os.WriteFile(filepath.Join(dir, ".burrow", "config.toml"), []byte(toml), 0o644)
	if got := projectClaudeConfigDir(dir); got != "/Users/x/.claude-work" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeLaunchForReadsConfig(t *testing.T) {
	a, dir := newChatApp(t)
	writeTestConfig(t, dir, map[string]any{
		"providers": []any{
			map[string]any{"id": "claude", "providerId": "claude", "enabled": true, "binary": "claude2", "args": []any{"-x"}},
		},
	})
	got := a.claudeLaunchFor(Chat{Model: "sonnet"}, "")
	want := ClaudeLaunch{Model: "sonnet", ProfileCommand: "claude2", ProfileArgs: "-x"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestBackfillChatLaunchSettings(t *testing.T) {
	a, dir := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	kept, err := a.CreateChat(Chat{WorkspaceID: 1, Model: "own-model"})
	if err != nil {
		t.Fatal(err)
	}
	filled, err := a.CreateChat(Chat{WorkspaceID: 1})
	if err != nil {
		t.Fatal(err)
	}
	k, f := strconv.FormatInt(kept.ID, 10), strconv.FormatInt(filled.ID, 10)
	writeTestConfig(t, dir, map[string]any{
		"chatModelByChat":      map[string]any{k: "legacy-model", f: "opus", "x:" + f: "scoped"},
		"chatEffortByChat":     map[string]any{f: "max"},
		"chatPermissionMode":   map[string]any{"byChat": map[string]any{f: "plan"}, "last": "default"},
		"chatProfileSelection": map[string]any{f: "claude-work", "999": "ghost"},
	})

	a.backfillChatLaunchSettings()

	byID := chatsByID(t, a)
	if got := byID[kept.ID].Model; got != "own-model" {
		t.Fatalf("backfill overwrote a set column: %q", got)
	}
	got := byID[filled.ID]
	if got.Model != "opus" || got.Effort != "max" || got.PermissionMode != "plan" || got.ProfileID != "claude-work" {
		t.Fatalf("not backfilled: %+v", got)
	}

	// Marker: a second run does nothing, even with different legacy values.
	if _, err := a.db.Exec(`UPDATE chats SET effort = '' WHERE id = ?`, filled.ID); err != nil {
		t.Fatal(err)
	}
	a.backfillChatLaunchSettings()
	if e := chatsByID(t, a)[filled.ID].Effort; e != "" {
		t.Fatalf("backfill ran twice: effort %q", e)
	}

	// SaveChats must not blank launch settings it never carries.
	row := chatsByID(t, a)[filled.ID]
	row.Model, row.ProfileID = "", ""
	if err := a.SaveChats([]Chat{row}); err != nil {
		t.Fatal(err)
	}
	if p := chatsByID(t, a)[filled.ID].ProfileID; p != "claude-work" {
		t.Fatalf("SaveChats blanked profile_id: %q", p)
	}
}

func chatsByID(t *testing.T, a *App) map[int64]Chat {
	t.Helper()
	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]Chat{}
	for _, c := range list {
		out[c.ID] = c
	}
	return out
}
