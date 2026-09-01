package main

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Provider probe: is this agent CLI installed, and which version?
//
// The Settings > Providers page shows one of these per configured instance. It
// runs on demand (opening the page, or the refresh button) and the frontend
// caches the result, so a probe is never on a hot path — hence the plain
// synchronous exec rather than a background poller.

type ProviderProbe struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

// A CLI's --version output is rarely just the number ("codex-cli 0.152.0",
// "claude 2.1.252 (Claude Code)"), so pull the first dotted number out.
var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?(?:[-+][0-9A-Za-z.-]+)?`)

// parseProviderVersion extracts a version from `--version` output. Empty when
// the tool printed something without a recognisable number — the caller still
// reports the binary as installed, since it clearly exists.
func parseProviderVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if m := versionRe.FindString(line); m != "" {
			return m
		}
	}
	return ""
}

// probeTimeout bounds a misbehaving CLI. A `--version` that hasn't answered in
// three seconds is not going to.
const probeTimeout = 3 * time.Second

// ProbeProvider reports whether `binary` exists and what version it claims.
// `cwd` only widens the search (a project's node_modules/.bin) and may be empty.
func (a *App) ProbeProvider(binary string, cwd string) ProviderProbe {
	name := strings.TrimSpace(binary)
	if name == "" {
		return ProviderProbe{Error: "no binary configured"}
	}

	path := resolveAgentBin(name, cwd)
	if path == "" {
		return ProviderProbe{Error: "not found on PATH"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	probe := ProviderProbe{Installed: true, Path: path}
	if ctx.Err() != nil {
		probe.Error = "version check timed out"
		return probe
	}
	// A non-zero exit means the CLI has no --version flag, not that it's
	// missing — we already know the binary is there.
	if err != nil && len(out) == 0 {
		probe.Error = err.Error()
		return probe
	}
	probe.Version = parseProviderVersion(string(out))
	return probe
}
