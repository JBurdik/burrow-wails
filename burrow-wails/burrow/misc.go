package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// Remaining small commands from src-tauri/src/lib.rs: system_stats,
// get/set_config_dirs, save_temp_image, is_pid_alive, format_source,
// set_tab_live_status, set_max_agents, set_burrow_mcp_max_depth.

// SystemStats reports this process' own memory/goroutine stats. Rust's
// version (via `sysinfo`) reports host-wide CPU/mem; matching that exactly
// would need a Go equivalent of sysinfo — deferred, this is what the
// stdlib gives us for free in the meantime.
type SystemStats struct {
	MemAllocMB   float64 `json:"memAllocMB"`
	NumGoroutine int     `json:"numGoroutine"`
}

func (a *App) SystemStats() SystemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return SystemStats{MemAllocMB: float64(m.Alloc) / 1024 / 1024, NumGoroutine: runtime.NumGoroutine()}
}

type ConfigDirs struct {
	Claude string `json:"claude"`
	Codex  string `json:"codex"`
	Copilot string `json:"copilot"`
}

func defaultConfigDirs() ConfigDirs {
	home, _ := os.UserHomeDir()
	return ConfigDirs{
		Claude:  filepath.Join(home, ".claude"),
		Codex:   filepath.Join(home, ".codex"),
		Copilot: filepath.Join(home, ".copilot"),
	}
}

func (a *App) GetConfigDirs() ConfigDirs {
	if a.configDirs != nil {
		return *a.configDirs
	}
	d := defaultConfigDirs()
	return d
}

func (a *App) SetConfigDirs(claude, codex, copilot string) ConfigDirs {
	d := ConfigDirs{Claude: claude, Codex: codex, Copilot: copilot}
	a.configDirs = &d
	return d
}

func (a *App) SaveTempImage(base64Data, ext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", err
	}
	dataDir, err := appDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "tmp-images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "img-*."+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (a *App) IsPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true // FindProcess always succeeds on non-unix; best-effort.
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (a *App) SetTabLiveStatus(ptyID, status string) {
	// Placeholder for future in-memory tab status tracking; the frontend
	// primarily derives status from pty-hook-{id} events already.
}

func (a *App) SetMaxAgents(n int) {
	a.maxAgents = n
}

func (a *App) SetBurrowMcpMaxDepth(n int) {
	a.burrowMcpMaxDepth = n
}
