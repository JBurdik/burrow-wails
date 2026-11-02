package main

import "testing"

func TestParseHotkey(t *testing.T) {
	for _, tc := range []struct {
		spec      string
		key, mods uint32
		ok        bool
	}{
		{"⌥Space", 0x31, carbonOption, true},
		{"⌃⌥K", 0x28, carbonControl | carbonOption, true},
		{"⌘⇧1", 0x12, carbonCmd | carbonShift, true},
		{"⌥F5", 0x60, carbonOption, true},
		{"⇧K", 0, 0, false},    // shift alone would eat typing
		{"Space", 0, 0, false}, // no modifier
		{"⌥", 0, 0, false},     // no key
		{"⌥ß", 0, 0, false},    // not in the table
	} {
		key, mods, err := parseHotkey(tc.spec)
		if (err == nil) != tc.ok || key != tc.key || mods != tc.mods {
			t.Errorf("parseHotkey(%q) = %#x, %#x, %v; want %#x, %#x, ok=%v", tc.spec, key, mods, err, tc.key, tc.mods, tc.ok)
		}
	}
}
