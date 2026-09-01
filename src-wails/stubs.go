package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

// Deliberately-stubbed commands (see plan phase 7 notes): Claude account/
// usage, remote-chat sync, and daemon admin telemetry aren't implemented
// yet. These no-op instead of throwing so the frontend doesn't hard-fail
// on load — call sites already treat empty/absent data as "unavailable",
// matching Tauri behavior when e.g. not logged in.
//
// Float/bubble *windows* were removed outright per user decision (Wails v2
// has no multi-window support; not worth a v3 migration or an overlay
// redesign right now) — see popOutTab/FloatBubble.vue removal. The
// snapshot *protocol* below is NOT float-window-specific despite the name:
// TaskLiveTerm.vue's read-only task live-view reuses the same request/
// send/grid events, so these three stay real (relayed via Wails events),
// not stubbed.

func (a *App) RequestFloatSnapshot(ptyID string) error {
	runtime.EventsEmit(a.ctx, "float-snap-req-"+ptyID)
	return nil
}

func (a *App) SendFloatSnapshot(ptyID string, data string, cols, rows int) error {
	runtime.EventsEmit(a.ctx, "float-snap-"+ptyID, map[string]any{"data": data, "cols": cols, "rows": rows})
	return nil
}

func (a *App) NotifyFloatGrid(ptyID string, cols, rows int) error {
	runtime.EventsEmit(a.ctx, "float-grid-"+ptyID, map[string]any{"cols": cols, "rows": rows})
	return nil
}

func (a *App) OpenGitPanelWindow() error { return nil }

func (a *App) RegisterTmuxWin(_winID, _ptyID string) error { return nil }

// --- Claude account/usage (needs the real API surface Claude Code exposes
// for this; not reverse-engineered yet) ---

type ClaudeAccountInfo struct {
	Email *string `json:"email,omitempty"`
}

func (a *App) ClaudeGetAccount(_cwd string) (ClaudeAccountInfo, error) {
	return ClaudeAccountInfo{}, nil
}

type ClaudeUsage struct {
	Available bool `json:"available"`
}

func (a *App) ClaudePlanUsage(_configDir string, _force bool) (ClaudeUsage, error) {
	return ClaudeUsage{Available: false}, nil
}

func (a *App) ClaudeUsage5h(_configDir string) (ClaudeUsage, error) {
	return ClaudeUsage{Available: false}, nil
}

// --- Remote chat sync (needs its own transport design) ---

func (a *App) RemoteSyncChat(_chat map[string]any) error { return nil }

// RemoteListChats now lives in remote.go. RemoteCreateChat is still a stub:
// creating a chat means writing config.json, which only the desktop may do.
func (a *App) RemoteCreateChat(_cwd string) (map[string]any, error) { return map[string]any{}, nil }

// --- Daemon admin (daemon.go doesn't exist yet — these operate through
// DaemonClient once it grows a stats/restart RPC; no-op for now) ---

func (a *App) DaemonStats() map[string]any              { return map[string]any{} }
func (a *App) CleanDaemon() int                         { return 0 }
func (a *App) KillOrphanSessions(_keepIDs []string) int { return 0 }
func (a *App) RestartDaemon() error                     { return nil }

func (a *App) FormatSource(_path, content, _cwd string) (string, error) {
	return content, nil // pass-through: no formatter invocation yet
}
