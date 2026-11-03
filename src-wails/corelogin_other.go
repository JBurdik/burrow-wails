//go:build !darwin && !headless

package main

// coreLoginItem: login items are a macOS (SMAppService) thing; Linux runs the
// core as a systemd user unit instead (plan 009, phase F).
func coreLoginItem(int) (string, error) { return "unsupported", nil }
