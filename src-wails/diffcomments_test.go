package main

import (
	"strings"
	"testing"
)

func TestComposeDiffNotesMarkdown(t *testing.T) {
	notes := []DiffComment{
		{File: "src/a.go", Line: 12, Side: "additions", Body: "```\nfunc a() {}\n```\n\nMissing nil check here."},
		{File: "src/b.go", Line: 40, Side: "deletions", Body: "Why was this removed?"},
		{File: "src/a.go", Line: 30, Side: "additions", Body: "Nit: rename this."},
	}

	out := composeDiffNotesMarkdown(notes)

	if !strings.HasPrefix(out, "## Review: 3 notes\n\n") {
		t.Fatalf("missing/wrong header, got:\n%s", out)
	}

	wantOrder := []string{
		"### src/a.go:12 (additions)",
		"Missing nil check here.",
		"### src/b.go:40 (deletions)",
		"Why was this removed?",
		"### src/a.go:30 (additions)",
		"Nit: rename this.",
	}
	lastIdx := -1
	for _, want := range wantOrder {
		idx := strings.Index(out, want)
		if idx < 0 {
			t.Fatalf("composed markdown missing %q, got:\n%s", want, out)
		}
		if idx < lastIdx {
			t.Fatalf("%q appeared out of order, got:\n%s", want, out)
		}
		lastIdx = idx
	}
}

func TestComposeDiffNotesMarkdownSingular(t *testing.T) {
	out := composeDiffNotesMarkdown([]DiffComment{{File: "x.go", Line: 1, Body: "hi"}})
	if !strings.HasPrefix(out, "## Review: 1 note\n\n") {
		t.Fatalf("expected singular header, got:\n%s", out)
	}
	// No side set — defaults to additions.
	if !strings.Contains(out, "### x.go:1 (additions)") {
		t.Fatalf("expected default side additions, got:\n%s", out)
	}
}

func TestComposeDiffNotesMarkdownEmpty(t *testing.T) {
	out := composeDiffNotesMarkdown(nil)
	if out != "## Review: 0 notes\n\n" {
		t.Fatalf("unexpected output for zero notes: %q", out)
	}
}
