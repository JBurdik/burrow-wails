package main

import (
	"encoding/json"
	"testing"
)

func TestParseTextGenerationSelection(t *testing.T) {
	cases := []struct {
		in, provider, model string
	}{
		{"claude::claude::claude-haiku", "claude", "claude-haiku"},
		{"codex::codex::gpt-5.2-codex", "codex", "gpt-5.2-codex"},
		{"gemini::gemini::", "gemini", ""},
		{"claude::claude-haiku", "claude", "claude-haiku"}, // early provider-aware format
		{"claude-haiku", "claude", "claude-haiku"},         // legacy preference
	}
	for _, tc := range cases {
		provider, model := parseTextGenerationSelection(tc.in)
		if provider != tc.provider || model != tc.model {
			t.Errorf("parseTextGenerationSelection(%q) = (%q, %q), want (%q, %q)", tc.in, provider, model, tc.provider, tc.model)
		}
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

func TestSanitizeChatTitle(t *testing.T) {
	cases := map[string]string{
		`"Fix PTY status dots"`:   "Fix PTY status dots",
		"Fix status dots.":        "Fix status dots",
		"First line\nsecond line": "First line",
		"":                        "",
		"   ":                     "",
		"A title that keeps going well past the sixty character limit imposed here": "A title that keeps going well past the sixty character limit",
	}
	for in, want := range cases {
		if got := sanitizeChatTitle(in); got != want {
			t.Errorf("sanitizeChatTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
