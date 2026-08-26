package main

// Deliberately-stubbed commands (see plan phase 7 notes): float/bubble
// windows need a Wails-appropriate redesign (single-window model) rather
// than a mechanical port, and Claude account/usage + remote-chat sync
// aren't implemented yet. These no-op instead of throwing so the frontend
// doesn't hard-fail on load — call sites already treat empty/absent data
// as "unavailable", matching Tauri behavior when e.g. not logged in.

// --- float windows (no-op: Wails is single-window) ---

func (a *App) OpenFloatWindow(_ptyID string) error                            { return nil }
func (a *App) CloseFloatWindow(_ptyID string) error                           { return nil }
func (a *App) SetFloatCorner(_ptyID, _corner string) error                    { return nil }
func (a *App) SnapFloatWindow(_ptyID string) error                            { return nil }
func (a *App) SyncFloatSize(_ptyID string) error                              { return nil }
func (a *App) RequestFloatSnapshot(_ptyID string) error                       { return nil }
func (a *App) SendFloatSnapshot(_ptyID, _data string, _cols, _rows int) error { return nil }
func (a *App) NotifyFloatGrid(_ptyID string, _cols, _rows int) error          { return nil }
func (a *App) OpenGitPanelWindow() error                                      { return nil }
func (a *App) RegisterTmuxWin(_winID, _ptyID string) error                    { return nil }

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

func (a *App) RemoteSyncChat(_chat map[string]any) error            { return nil }
func (a *App) RemoteListChats() ([]map[string]any, error)           { return nil, nil }
func (a *App) RemoteCreateChat(_cwd string) (map[string]any, error) { return map[string]any{}, nil }

// --- Daemon admin (daemon.go doesn't exist yet — these operate through
// DaemonClient once it grows a stats/restart RPC; no-op for now) ---

func (a *App) DaemonStats() map[string]any              { return map[string]any{} }
func (a *App) CleanDaemon() int                         { return 0 }
func (a *App) KillOrphanSessions(_keepIDs []string) int { return 0 }
func (a *App) RestartDaemon() error                     { return nil }
func (a *App) RepairAgentStatus() int                   { return 0 }

func (a *App) FormatSource(_path, content, _cwd string) (string, error) {
	return content, nil // pass-through: no formatter invocation yet
}
