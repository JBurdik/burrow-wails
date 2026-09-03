package main

import (
	"context"
	"encoding/json"
	"net/http"
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

// ProviderLatest reports the latest published version of a provider's npm
// package, for comparison against the installed ProviderProbe.Version.
type ProviderLatest struct {
	Version string `json:"version"`
	Error   string `json:"error"`
}

// LatestNpmVersion queries the npm registry's "latest" dist-tag for pkg.
// Same timeout budget as ProbeProvider — an on-demand check, not a poller.
func (a *App) LatestNpmVersion(pkg string) ProviderLatest {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ProviderLatest{Error: "no package configured"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/"+pkg+"/latest", nil)
	if err != nil {
		return ProviderLatest{Error: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ProviderLatest{Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProviderLatest{Error: "registry returned " + resp.Status}
	}

	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ProviderLatest{Error: err.Error()}
	}
	if body.Version == "" {
		return ProviderLatest{Error: "no version in registry response"}
	}
	return ProviderLatest{Version: body.Version}
}
