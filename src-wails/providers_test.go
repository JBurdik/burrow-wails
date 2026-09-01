package main

import "testing"

func TestParseProviderVersion(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{"bare number", "0.152.0\n", "0.152.0"},
		{"claude style", "2.1.252 (Claude Code)\n", "2.1.252"},
		{"prefixed name", "codex-cli 0.152.0\n", "0.152.0"},
		{"v prefix", "gemini v1.4\n", "1.4"},
		{"prerelease", "opencode 1.2.3-beta.4\n", "1.2.3-beta.4"},
		{"skips a leading banner line", "Loading config…\naider 0.60.1\n", "0.60.1"},
		{"no number at all", "usage: tool [options]\n", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseProviderVersion(c.out); got != c.want {
				t.Fatalf("parseProviderVersion(%q) = %q, want %q", c.out, got, c.want)
			}
		})
	}
}

func TestProbeProviderRejectsEmptyBinary(t *testing.T) {
	var app App
	got := app.ProbeProvider("   ", "")
	if got.Installed || got.Error == "" {
		t.Fatalf("empty binary should be reported as not installed with an error, got %+v", got)
	}
}

func TestProbeProviderReportsMissingBinary(t *testing.T) {
	var app App
	got := app.ProbeProvider("burrow-definitely-not-a-real-binary", "")
	if got.Installed {
		t.Fatalf("missing binary reported as installed: %+v", got)
	}
	if got.Error != "not found on PATH" {
		t.Fatalf("unexpected error: %q", got.Error)
	}
}

func TestProbeProviderFindsRealBinary(t *testing.T) {
	var app App
	// /bin/sh is on every machine this app runs on, and answers --version on
	// macOS (bash) — but the assertion only needs "found and executed".
	got := app.ProbeProvider("sh", "")
	if !got.Installed || got.Path == "" {
		t.Fatalf("sh should resolve: %+v", got)
	}
}
