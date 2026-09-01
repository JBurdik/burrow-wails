package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
)

// Font enumeration for the Settings font pickers. The presets baked into the
// frontend only cover a handful of families; this lists what is actually
// installed so the dropdowns aren't a guessing game.

var (
	fontsOnce   sync.Once
	fontsCached []string
)

func fontDirs() []string {
	dirs := []string{"/System/Library/Fonts", "/Library/Fonts"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
	}
	return dirs
}

// ListFonts returns the installed font families, sorted. Scanning + parsing
// every font file takes a moment, so the result is computed once per launch.
func ListFonts() []string {
	fontsOnce.Do(func() { fontsCached = scanFonts(fontDirs()) })
	return fontsCached
}

func (a *App) ListFonts() []string { return ListFonts() }

func scanFonts(dirs []string) []string {
	seen := map[string]bool{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // an unreadable font dir just contributes nothing
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			for _, name := range fontFamilies(path) {
				// Apple's internal faces are family names prefixed with a dot
				// (".Aqua Kana", ".Apple Color Emoji UI") — not for user picking.
				if strings.HasPrefix(name, ".") {
					continue
				}
				seen[name] = true
			}
			return nil
		})
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return compareFold(out[i], out[j]) < 0 })
	return out
}

// fontFamilies reads the family name(s) out of a font file's `name` table. A
// .ttc/.otc holds several faces, hence the slice. Unparseable files fall back
// to the filename stem, which is close enough for a picker.
func fontFamilies(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var fonts []*sfnt.Font
	if coll, err := sfnt.ParseCollection(data); err == nil {
		for i := 0; i < coll.NumFonts(); i++ {
			if f, err := coll.Font(i); err == nil {
				fonts = append(fonts, f)
			}
		}
	}
	var buf sfnt.Buffer
	var out []string
	for _, f := range fonts {
		name, err := f.Name(&buf, sfnt.NameIDFamily)
		if err != nil || strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(name))
	}
	if len(out) == 0 {
		if stem := filenameFamily(path); stem != "" {
			return []string{stem}
		}
		return nil
	}
	return out
}

// filenameFamily turns "Inter-SemiBoldItalic.ttf" into "Inter".
func filenameFamily(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if i := strings.IndexAny(stem, "-_"); i > 0 {
		stem = stem[:i]
	}
	return strings.TrimSpace(stem)
}

// compareFold orders family names case-insensitively (exported for the sort
// assertion in tests to reuse the exact ordering scanFonts applies).
func compareFold(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	switch {
	case la < lb:
		return -1
	case la > lb:
		return 1
	}
	return 0
}
