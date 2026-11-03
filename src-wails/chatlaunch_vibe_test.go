package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Vibe ships its ACP adapter as a separate program: a chat runs vibe-acp,
// not the TUI binary, unless the instance overrides the binary.
func TestRPCLaunchUsesCatalogChatBinary(t *testing.T) {
	c := Chat{ID: 1, AgentKind: "mistral-vibe"}
	inst := providerInstance{ID: "mistral-vibe", ProviderID: "mistral-vibe", Transport: "acp"}
	opts, err := resolveRPCLaunch(c, "acp", []providerInstance{inst}, nil, "/tmp", "", "")
	if err != nil || opts.Command != "vibe-acp" {
		t.Fatalf("default: %q %v", opts.Command, err)
	}
	inst.Binary = "/opt/vibe-acp"
	if opts, _ = resolveRPCLaunch(c, "acp", []providerInstance{inst}, nil, "/tmp", "", ""); opts.Command != "/opt/vibe-acp" {
		t.Fatalf("override: %q", opts.Command)
	}
}

func TestAcpEffortID(t *testing.T) {
	opt := func(id, cat string) any { return map[string]any{"id": id, "category": cat} }
	for _, c := range []struct {
		in   []any
		want string
	}{
		{[]any{opt("model", "model"), opt("thinking", "thinking")}, "thinking"}, // Mistral Vibe
		{[]any{opt("reasoning", "thought_level")}, "reasoning"},                 // ACP spec
		{[]any{opt("effort", "thought_level")}, ""},                             // already Burrow's name
		{[]any{opt("model", "model")}, ""},
	} {
		if got := acpEffortID(c.in); got != c.want {
			t.Errorf("acpEffortID(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMcpStdioServerSurvivesSpaces(t *testing.T) {
	if s := mcpStdioServer("burrow", "/opt/burrow-mcp"); s["command"] != "/opt/burrow-mcp" {
		t.Fatalf("plain path rewritten: %v", s)
	}
	dir := filepath.Join(t.TempDir(), "Burrow Dev.app")
	_ = os.MkdirAll(dir, 0o755)
	bin := filepath.Join(dir, "burrow-mcp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := mcpStdioServer("burrow", bin)
	out, err := exec.Command(s["command"].(string), s["args"].([]string)...).Output()
	if s["command"] != "/bin/sh" || err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("spaced path: %v -> %q %v", s, out, err)
	}
}

func TestIsLiveUserEcho(t *testing.T) {
	echo := map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "user_message_chunk"}}}
	agent := map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk"}}}
	if !isLiveUserEcho(echo) || isLiveUserEcho(agent) {
		t.Fatal("echo detection")
	}
}
