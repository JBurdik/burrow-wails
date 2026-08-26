package main

import (
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickDirectory/PickFile/PickFiles back the frontend's plugin-dialog
// open() shim (src/lib/wailsCompat/dialog.ts) with Wails' native dialog
// runtime calls. filterName/filterExts mirror Tauri's `filters: [{name,
// extensions}]` (single filter group — every call-site in src/ uses one).

func dialogFilters(filterName string, filterExts []string) []runtime.FileFilter {
	if filterName == "" || len(filterExts) == 0 {
		return nil
	}
	pattern := "*." + strings.Join(filterExts, ";*.")
	return []runtime.FileFilter{{DisplayName: filterName, Pattern: pattern}}
}

// trimQuotes strips literal surrounding double-quote characters that
// macOS's NSOpenPanel-backed dialog can embed in the returned path.
func trimQuotes(s string) string {
	return strings.Trim(s, "\"")
}

func (a *App) PickDirectory() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{})
	return trimQuotes(path), err
}

func (a *App) PickFile(filterName string, filterExts []string) (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: dialogFilters(filterName, filterExts),
	})
	return trimQuotes(path), err
}

func (a *App) PickFiles(filterName string, filterExts []string) ([]string, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: dialogFilters(filterName, filterExts),
	})
	for i, p := range paths {
		paths[i] = trimQuotes(p)
	}
	return paths, err
}
