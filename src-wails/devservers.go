package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DevServer is one TCP listener whose process cwd is under a workspace —
// the "dev server" the Right Panel's Dev servers surface lists and can kill.
type DevServer struct {
	Pid     int    `json:"pid"`
	Port    int    `json:"port"`
	Addr    string `json:"addr"`
	Command string `json:"command"`
}

type devListener struct {
	pid  int
	port string
	addr string
}

// parseDevListeners parses `lsof -nP -iTCP -sTCP:LISTEN -F pn` output. Each
// record starts with a p<pid> line; the n<addr> lines that follow belong to
// that pid until the next p line. One pid commonly listens on the same port
// on several fds (v4 and v6 both bound), so this dedupes by (pid, port).
func parseDevListeners(out string) []devListener {
	var result []devListener
	seen := map[string]bool{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			addr := line[1:]
			idx := strings.LastIndex(addr, ":")
			if idx < 0 || pid == 0 {
				continue
			}
			port := addr[idx+1:]
			key := strconv.Itoa(pid) + ":" + port
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, devListener{pid: pid, port: port, addr: addr})
		}
	}
	return result
}

// parseDevCwds parses `lsof -a -p <pids> -d cwd -F n` output: p<pid> then
// n<cwd> pairs, one cwd per pid.
func parseDevCwds(out string) map[int]string {
	result := map[int]string{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid != 0 {
				result[pid] = line[1:]
			}
		}
	}
	return result
}

// underWorkspace is a path-separator-aware prefix check: workspace "/a/b"
// must not match cwd "/a/bc".
func underWorkspace(cwd, workspacePath string) bool {
	cwd = filepath.Clean(cwd)
	workspacePath = filepath.Clean(workspacePath)
	return cwd == workspacePath || strings.HasPrefix(cwd, workspacePath+string(filepath.Separator))
}

// isBurrowCommand excludes Burrow's own processes — the daemon, the MCP
// sidecar, and a `wails dev` run — which would otherwise list themselves as
// "dev servers" for whatever workspace happens to be this repo. The app's
// own pid is excluded separately by the caller; this is the fallback for
// processes the daemon pid isn't reachable enough to name directly.
func isBurrowCommand(command, selfExeBase string) bool {
	for _, marker := range []string{"burrow-daemon", "burrow-mcp", "wails dev"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return selfExeBase != "" && strings.Contains(command, selfExeBase)
}

// ListDevServers finds every TCP listener whose process cwd is under
// workspacePath.
func (a *App) ListDevServers(workspacePath string) ([]DevServer, error) {
	lsofOut, _ := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pn").Output()
	listeners := parseDevListeners(string(lsofOut))
	servers := []DevServer{}
	if len(listeners) == 0 {
		return servers, nil
	}

	pids := make([]string, 0, len(listeners))
	seenPid := map[int]bool{}
	for _, l := range listeners {
		if !seenPid[l.pid] {
			seenPid[l.pid] = true
			pids = append(pids, strconv.Itoa(l.pid))
		}
	}
	cwdOut, _ := exec.Command("lsof", "-a", "-p", strings.Join(pids, ","), "-d", "cwd", "-F", "n").Output()
	cwds := parseDevCwds(string(cwdOut))

	selfPid := os.Getpid()
	selfExeBase := ""
	if exe, err := os.Executable(); err == nil {
		selfExeBase = filepath.Base(exe)
	}

	for _, l := range listeners {
		if l.pid == selfPid {
			continue
		}
		cwd, ok := cwds[l.pid]
		if !ok || !underWorkspace(cwd, workspacePath) {
			continue
		}
		// ps, not lsof's own truncated `c` field, for the full command line.
		cmdOut, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(l.pid)).Output()
		if err != nil {
			continue
		}
		command := strings.TrimSpace(string(cmdOut))
		if command == "" || isBurrowCommand(command, selfExeBase) {
			continue
		}
		port, _ := strconv.Atoi(l.port)
		servers = append(servers, DevServer{Pid: l.pid, Port: port, Addr: l.addr, Command: command})
	}
	return servers, nil
}

// KillDevServer signals the process GROUP, not just pid: a dev server is
// almost always a shell that forked the real listener, and signalling the
// pid alone orphans it. SIGTERM first, then SIGKILL after a 3s grace period
// if it hasn't gone.
func (a *App) KillDevServer(pid int) error {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pgid, 0) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
