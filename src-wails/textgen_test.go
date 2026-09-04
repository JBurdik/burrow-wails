package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseTextGenerationSelection(t *testing.T) {
	cases := []struct {
		in, provider, model, effort string
	}{
		{"claude::claude::claude-opus-5::high", "claude", "claude-opus-5", "high"},
		{"codex::codex::gpt-5.6-luna::low", "codex", "gpt-5.6-luna", "low"},
		{"claude::claude::claude-haiku", "claude", "claude-haiku", ""},
		{"codex::codex::gpt-5.2-codex", "codex", "gpt-5.2-codex", ""},
		{"gemini::gemini::", "gemini", "", ""},
		{"claude::claude-haiku", "claude", "claude-haiku", ""}, // early provider-aware format
		{"claude-haiku", "claude", "claude-haiku", ""},         // legacy preference
	}
	for _, tc := range cases {
		got := parseTextGenerationSelection(tc.in)
		if got.provider != tc.provider || got.model != tc.model || got.effort != tc.effort {
			t.Errorf("parseTextGenerationSelection(%q) = %+v, want (%q, %q, %q)", tc.in, got, tc.provider, tc.model, tc.effort)
		}
	}
}

func TestClaudeCliEffort(t *testing.T) {
	cases := map[string]string{
		"low":        "low",
		"max":        "max",
		"ultracode":  "xhigh", // a settings flag that pairs with xhigh, not an effort
		"ultrathink": "",      // a prompt-prefix mode, never a --effort value
		"":           "",
		"bogus":      "",
	}
	for in, want := range cases {
		if got := claudeCliEffort(in); got != want {
			t.Errorf("claudeCliEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextGenPolicyFor(t *testing.T) {
	if p := textGenPolicyFor("conventional_commits"); !strings.Contains(p.commitInstructions, "Conventional Commits") {
		t.Errorf("conventional_commits policy lost its commit instructions: %+v", p)
	}
	if p := textGenPolicyFor("repo_conventions"); !p.inferRepositoryConventions {
		t.Errorf("repo_conventions policy must sample the repo: %+v", p)
	}
	// A preference written by a newer build must not break generation.
	for _, kind := range []string{"", "default", "from-the-future"} {
		if p := textGenPolicyFor(kind); p.commitInstructions != "" || p.inferRepositoryConventions {
			t.Errorf("textGenPolicyFor(%q) = %+v, want the empty default policy", kind, p)
		}
	}
}

func TestPolicyInstruction(t *testing.T) {
	if got := policyInstruction("   "); got != nil {
		t.Errorf("policyInstruction(blank) = %q, want no section", got)
	}
	got := policyInstruction("Use Conventional Commits.")
	if len(got) != 3 || got[1] != "Additional instructions:" || got[2] != "Use Conventional Commits." {
		t.Errorf("policyInstruction() = %q", got)
	}
}

func TestLimitSection(t *testing.T) {
	if got := limitSection("short", 100); got != "short" {
		t.Errorf("limitSection kept-as-is = %q", got)
	}
	got := limitSection("0123456789", 4)
	if got != "0123\n\n[truncated]" {
		t.Errorf("limitSection truncated = %q", got)
	}
}

func TestExtractGeneratedJSON(t *testing.T) {
	for _, in := range [][]byte{
		[]byte(`{"subject":"Add picker","body":""}`),
		[]byte(`{"response":"{\"title\":\"Add picker\"}"}`),
		[]byte("Some provider output\n{\"title\":\"Add picker\"}\n"),
	} {
		out, err := extractGeneratedJSON(in)
		if err != nil || !json.Valid(out) {
			t.Errorf("extractGeneratedJSON(%q) = %q, %v", in, out, err)
		}
	}
}

func TestSanitizeCommitSubject(t *testing.T) {
	cases := map[string]string{
		"Add the model picker.":  "Add the model picker",
		"  ":                     "Update project files",
		"Add picker\nand a body": "Add picker",
		strings.Repeat("a", 80):  strings.Repeat("a", 72),
	}
	for in, want := range cases {
		if got := sanitizeCommitSubject(in); got != want {
			t.Errorf("sanitizeCommitSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizePrTitle(t *testing.T) {
	cases := map[string]string{
		`"Add the model picker"`:   "Add the model picker",
		"Add picker\n\n## Summary": "Add picker",
		"":                         "Update project changes",
		"   \n  Add picker  ":      "Add picker",
	}
	for in, want := range cases {
		if got := sanitizePrTitle(in); got != want {
			t.Errorf("sanitizePrTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeChatTitle(t *testing.T) {
	cases := map[string]string{
		`"Fix PTY status dots"`:   "Fix PTY status dots",
		"Fix status dots.":        "Fix status dots",
		"First line\nsecond line": "First line",
		"Fix   the    dots":       "Fix the dots",
		"":                        "",
		"   ":                     "",
		"A title that keeps going well past the fifty character limit imposed here": "A title that keeps going well past the fifty ch...",
	}
	for in, want := range cases {
		if got := sanitizeChatTitle(in); got != want {
			t.Errorf("sanitizeChatTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeBranchFragment(t *testing.T) {
	cases := map[string]string{
		"Fix PTY status dots":      "fix-pty-status-dots",
		"feature/Add Model Picker": "feature/add-model-picker",
		"   ":                      "update",
		"!!!":                      "update",
		"Add   picker!! (again)":   "add-picker-again",
		// slug is cut at 64 chars, t3code's limit, then trailing separators go
		strings.Repeat("branch ", 20): strings.Repeat("branch-", 9) + "b",
	}
	for in, want := range cases {
		got := sanitizeBranchFragment(in)
		if got != want {
			t.Errorf("sanitizeBranchFragment(%q) = %q, want %q", in, got, want)
		}
		if len(got) > 64 {
			t.Errorf("sanitizeBranchFragment(%q) = %q, longer than 64 chars", in, got)
		}
	}
}

func TestChatTitleFromOutput(t *testing.T) {
	cases := map[string]string{
		`{"title":"Fix status dots"}`:            "Fix status dots",
		"```json\n{\"title\":\"Fix dots\"}\n```": "Fix dots",        // codex fences its JSON
		"Fix status dots":                        "Fix status dots", // …or skips JSON entirely
		"":                                       "",
	}
	for in, want := range cases {
		if got := chatTitleFromOutput([]byte(in)); got != want {
			t.Errorf("chatTitleFromOutput(%q) = %q, want %q", in, got, want)
		}
	}
}
