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

func (a *App) PickDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{})
}

func (a *App) PickFile(filterName string, filterExts []string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: dialogFilters(filterName, filterExts),
	})
}

func (a *App) PickFiles(filterName string, filterExts []string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: dialogFilters(filterName, filterExts),
	})
}
