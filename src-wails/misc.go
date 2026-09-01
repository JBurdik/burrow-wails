package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// Remaining small commands from src-tauri/src/lib.rs: system_stats,
// save_temp_image, is_pid_alive, format_source,
// set_tab_live_status, set_max_agents, set_burrow_mcp_max_depth.

// SystemStats reports host-wide CPU/memory, matching the shape the title bar's
// gauge reads (`cpu_percent`/`mem_used`/`mem_total`). The Rust build used
// `sysinfo`; gopsutil is its Go equivalent.
type SystemStats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
}

func (a *App) SystemStats() SystemStats {
	var out SystemStats
	// 0 interval = usage since the previous call (since boot on the first one),
	// which is what a 2 s poll wants — a blocking sample would stall the caller.
	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		out.CPUPercent = pct[0]
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		out.MemUsed, out.MemTotal = vm.Used, vm.Total
	}
	return out
}

// HomeDir is the user's home dir, for path pickers that show a "~" root.
func (a *App) HomeDir() string {
	home, _ := os.UserHomeDir()
	return home
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
