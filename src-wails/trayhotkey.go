package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The tray panel's global shortcut, uiPrefs.trayHotkey: modifier glyphs then
// one physical key — "⌥Space", "⌃⌥K", "⌘⇧1", "⌥F5". Recorded from
// KeyboardEvent.code in Settings (⌥ turns e.key into a symbol). "" = off.
const defaultTrayHotkey = "⌥Space"

// Carbon modifier masks (Events.h) and US-layout virtual key codes.
const (
	carbonCmd     = 0x0100
	carbonShift   = 0x0200
	carbonOption  = 0x0800
	carbonControl = 0x1000
)

var carbonKeys = map[string]uint32{
	"A": 0x00, "S": 0x01, "D": 0x02, "F": 0x03, "H": 0x04, "G": 0x05, "Z": 0x06, "X": 0x07,
	"C": 0x08, "V": 0x09, "B": 0x0B, "Q": 0x0C, "W": 0x0D, "E": 0x0E, "R": 0x0F, "Y": 0x10,
	"T": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15, "6": 0x16, "5": 0x17, "9": 0x19,
	"7": 0x1A, "8": 0x1C, "0": 0x1D, "O": 0x1F, "U": 0x20, "I": 0x22, "P": 0x23, "L": 0x25,
	"J": 0x26, "K": 0x28, "N": 0x2D, "M": 0x2E,
	"Space": 0x31, "Enter": 0x24, "Tab": 0x30, "Escape": 0x35, "Backquote": 0x32,
	"ArrowLeft": 0x7B, "ArrowRight": 0x7C, "ArrowDown": 0x7D, "ArrowUp": 0x7E,
	"F1": 0x7A, "F2": 0x78, "F3": 0x63, "F4": 0x76, "F5": 0x60, "F6": 0x61,
	"F7": 0x62, "F8": 0x64, "F9": 0x65, "F10": 0x6D, "F11": 0x67, "F12": 0x6F,
}

// parseHotkey turns a spec into a Carbon key code + modifier mask. A global
// shortcut needs at least one of ⌘⌥⌃ — a bare key would swallow typing.
func parseHotkey(spec string) (key, mods uint32, err error) {
	rest := spec
	for {
		switch {
		case strings.HasPrefix(rest, "⌘"):
			mods |= carbonCmd
		case strings.HasPrefix(rest, "⌥"):
			mods |= carbonOption
		case strings.HasPrefix(rest, "⌃"):
			mods |= carbonControl
		case strings.HasPrefix(rest, "⇧"):
			mods |= carbonShift
		default:
			k, ok := carbonKeys[rest]
			if !ok {
				return 0, 0, fmt.Errorf("unsupported key %q", rest)
			}
			if mods&(carbonCmd|carbonOption|carbonControl) == 0 {
				return 0, 0, fmt.Errorf("%q needs ⌘, ⌥ or ⌃", spec)
			}
			return k, mods, nil
		}
		rest = rest[len("⌘"):] // every glyph above is 3 bytes in UTF-8
	}
}

// trayHotkeySpec reads uiPrefs.trayHotkey fresh: absent = the default, ""
// = off, an unreadable config = the default (a hotkey is harmless).
func (a *App) trayHotkeySpec() string {
	raw, err := a.ReadConfig()
	if err != nil {
		return defaultTrayHotkey
	}
	var cfg struct {
		UI struct {
			Hotkey *string `json:"trayHotkey"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil || cfg.UI.Hotkey == nil {
		return defaultTrayHotkey
	}
	return *cfg.UI.Hotkey
}
