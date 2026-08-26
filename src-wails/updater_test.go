package main

import "testing"

func TestVersionGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.1", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.1", false},
		{"0.2.0", "0.1.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.10.0", "0.9.0", true}, // numeric, not lexicographic
		{"0.1.10", "0.1.9", true}, // ditto
		{"0.2", "0.2.0", false},   // ragged lengths pad with 0
		{"0.2.1", "0.2", true},    // ditto
	}
	for _, c := range cases {
		if got := versionGreater(c.a, c.b); got != c.want {
			t.Errorf("versionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
