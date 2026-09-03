package main

import "testing"

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
