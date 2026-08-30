package main

import "testing"

// `path:line:text` is ambiguous once the matched text has colons of its own —
// only the first two are separators.
func TestParseGrepLine(t *testing.T) {
	hit, ok := parseGrepLine("src/a.ts:42:const url = \"http://x\";")
	if !ok || hit.Path != "src/a.ts" || hit.Line != 42 || hit.Text != "const url = \"http://x\";" {
		t.Fatalf("got %+v ok=%v", hit, ok)
	}
	for _, bad := range []string{"", "no colons", "path:notanumber:text", ":5:x"} {
		if _, ok := parseGrepLine(bad); ok {
			t.Fatalf("accepted malformed line %q", bad)
		}
	}
}
