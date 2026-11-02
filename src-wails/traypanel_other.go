//go:build !darwin && !headless

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func prepareTrayPanel(*application.WebviewWindow)          {}
func setTrayPanelMovable(*application.WebviewWindow, bool) {}
func registerTrayHotkey(*App)                              {}
func setTrayHotkey(string) error                           { return nil }
