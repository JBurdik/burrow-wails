package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestClaudeModelsMatchFrontend(t *testing.T) {
	src, err := os.ReadFile("../src/lib/chatModels.ts")
	if err != nil {
		t.Fatal(err)
	}
	block := string(src)[strings.Index(string(src), "claude: ["):]
	block = block[:strings.Index(block, "],\n};")]
	var want []string
	for _, m := range regexp.MustCompile(`id: "([^"]+)"`).FindAllStringSubmatch(block, -1) {
		want = append(want, m[1])
	}
	var got []string
	for _, m := range claudeModels {
		got = append(got, m.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("claudeModels drifted from chatModels.ts:\n go: %v\n ts: %v", got, want)
	}
}

func TestBuildAgentCaps(t *testing.T) {
	var seen []capsModel
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		seen = append(seen, capsModel{ID: id})
	}
	cfg := capsConfig{Seen: map[string][]capsModel{"cx": seen}}
	cfg.Providers = []providerInstance{
		{ID: "cl", Name: "Claude", Kind: "claude", ProviderID: "claude", Enabled: true},
		{ID: "cx", Name: "Codex", Kind: "codex", ProviderID: "codex", Transport: "codex-app-server", Enabled: true},
		{ID: "gm", Name: "Gemini", Kind: "gemini", ProviderID: "gemini", Transport: "acp", Enabled: true},
		{ID: "ai", Name: "Aider", Kind: "aider", ProviderID: "aider", Transport: "none", Enabled: false},
	}
	have := map[string]bool{"claude": true, "codex": true}
	look := func(name, _ string) string {
		if have[name] {
			return "/bin/" + name
		}
		return ""
	}
	caller := &Chat{ID: 7, AgentKind: "cx", Transport: "codex-app-server", Model: "z", PermissionMode: "auto"}
	got := buildAgentCaps(cfg, caller, "", look)

	if got.Caller == nil || got.Caller.Transport != "codex-app-server" || got.Caller.PermissionMode != "auto" {
		t.Fatalf("caller = %+v", got.Caller)
	}
	by := map[string]capsAgent{}
	for _, a := range got.Agents {
		by[a.ID] = a
	}
	if cx := by["cx"]; !cx.Default || !cx.InheritsCaller || !cx.Available || !cx.Profiles ||
		cx.Models[0].ID != "z" || len(cx.Models) != shortModelList || cx.OmittedModels != 3 {
		t.Errorf("codex = %+v", cx)
	}
	if cl := by["cl"]; cl.Default || cl.InheritsCaller || cl.Transport != "claude-cli" || len(cl.Models) != shortModelList || cl.PermissionModes[0] != "plan" {
		t.Errorf("claude = %+v", cl)
	}
	if gm := by["gm"]; gm.Available || !strings.Contains(gm.Reason, "gemini not found") || gm.Profiles || gm.ModelsNote == "" {
		t.Errorf("gemini = %+v", gm)
	}
	if ai := by["ai"]; ai.Chat || ai.Transport != "tab" || ai.Available || ai.Reason != "disabled in Settings > Providers" {
		t.Errorf("aider = %+v", ai)
	}
	if len(got.Profiles) != 3 || len(got.Rules) == 0 {
		t.Errorf("profiles/rules = %d/%d", len(got.Profiles), len(got.Rules))
	}

	// One agent by name: full model list.
	one := buildAgentCaps(cfg, nil, "codex", look)
	if len(one.Agents) != 1 || len(one.Agents[0].Models) != 10 || one.Agents[0].OmittedModels != 0 || one.Caller != nil {
		t.Errorf("only codex = %+v", one.Agents)
	}
	// No caller: spawn's default chat agent (claude).
	if none := buildAgentCaps(cfg, nil, "", look); !none.Agents[0].Default {
		t.Errorf("default without caller = %+v", none.Agents[0])
	}
}
