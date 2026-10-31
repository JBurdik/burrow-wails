package main

import "strings"

// PickDirectory/PickFile/PickFiles back the frontend's plugin-dialog
// open() shim (src/lib/wailsCompat/dialog.ts) with the native dialog
// (pickPaths, desktop.go). filterName/filterExts mirror Tauri's `filters:
// [{name, extensions}]` (single filter group — every call-site in src/ uses one).

// trimQuotes strips literal surrounding double-quote characters that
// macOS's NSOpenPanel-backed dialog can embed in the returned path.
func trimQuotes(s string) string {
	return strings.Trim(s, "\"")
}

func pickOne(dirs bool, filterName string, filterExts []string) (string, error) {
	paths, err := pickPaths(dirs, false, filterName, filterExts)
	if len(paths) == 0 {
		return "", err
	}
	return trimQuotes(paths[0]), err
}

func (a *App) PickDirectory() (string, error) {
	return pickOne(true, "", nil)
}

func (a *App) PickFile(filterName string, filterExts []string) (string, error) {
	return pickOne(false, filterName, filterExts)
}

func (a *App) PickFiles(filterName string, filterExts []string) ([]string, error) {
	paths, err := pickPaths(false, true, filterName, filterExts)
	for i, p := range paths {
		paths[i] = trimQuotes(p)
	}
	return paths, err
}
