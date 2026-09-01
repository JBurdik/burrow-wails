package main

import (
	"os"
	"slices"
	"testing"
)

func TestFilenameFamily(t *testing.T) {
	for in, want := range map[string]string{
		"/x/Inter-SemiBoldItalic.ttf": "Inter",
		"/x/Menlo.ttc":                "Menlo",
		"/x/JetBrains_Mono.otf":       "JetBrains",
	} {
		if got := filenameFamily(in); got != want {
			t.Errorf("filenameFamily(%q) = %q, want %q", in, got, want)
		}
	}
}

// The name-table read is the part that can silently regress into filename
// stems, so assert it against a font every macOS has.
func TestFontFamiliesReadsNameTable(t *testing.T) {
	const path = "/System/Library/Fonts/Menlo.ttc"
	if _, err := os.Stat(path); err != nil {
		t.Skip("Menlo.ttc not present")
	}
	got := fontFamilies(path)
	if !slices.Contains(got, "Menlo") {
		t.Errorf("fontFamilies(%s) = %v, want it to contain %q", path, got, "Menlo")
	}
}

func TestScanFontsFindsSystemFamilies(t *testing.T) {
	got := scanFonts([]string{"/System/Library/Fonts"})
	if len(got) < 10 {
		t.Fatalf("scanFonts found %d families, want at least 10: %v", len(got), got)
	}
	if !slices.IsSortedFunc(got, func(a, b string) int { return compareFold(a, b) }) {
		t.Errorf("scanFonts result is not sorted: %v", got)
	}
}
