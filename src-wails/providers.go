package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
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

// updateTimeout bounds `npm install -g` / `brew upgrade`, which fetch a
// package rather than just answering a question — a plain probe's 3s budget
// would fail every time.
const updateTimeout = 120 * time.Second

type ProviderUpdateResult struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error"`
}

// isHomebrewInstallPath reports whether a resolved binary was installed by
// Homebrew, ported from t3code's isHomebrewCommandPath. `binaryPath` should
// be symlink-resolved first: Homebrew's PATH shims in bin/ are themselves
// symlinks into Cellar/Caskroom, and a naive substring check on the shim
// would still catch /opt/homebrew/bin/ or /usr/local/bin/ — kept here too as
// a fallback for a formula that installs straight into bin/ with no symlink.
func isHomebrewInstallPath(path string) bool {
	p := strings.ToLower(path)
	return strings.Contains(p, "/cellar/") ||
		strings.Contains(p, "/caskroom/") ||
		strings.HasPrefix(p, "/opt/homebrew/bin/") ||
		strings.HasPrefix(p, "/usr/local/bin/")
}

// homebrewTapFor extracts the tap ("user/repo") from a formula reference of
// the form "user/repo/name"; a bare core-formula name ("codex") needs none.
func homebrewTapFor(formula string) (string, bool) {
	parts := strings.Split(formula, "/")
	if len(parts) != 3 {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

// UpdateProvider installs the latest version of a provider CLI. It resolves
// `binary` on PATH the same way ProbeProvider does, then picks the update
// command from *how that binary got there* rather than trusting the
// catalog's default package manager: a `claude` resolved out of a Homebrew
// Cellar path was installed with `brew install claude-code`, and `npm
// install -g` on it would fight Homebrew's own symlinks on every future
// `brew upgrade` — only `brew upgrade <formula>` is the actual owner. Falls
// back to `npm install -g <pkg>@latest` for everything else (the common
// case, and the only option when a provider has no Homebrew formula).
func (a *App) UpdateProvider(binary string, pkg string, homebrewFormula string, cwd string) ProviderUpdateResult {
	binary = strings.TrimSpace(binary)
	pkg = strings.TrimSpace(pkg)
	homebrewFormula = strings.TrimSpace(homebrewFormula)

	var exe string
	var args []string

	if homebrewFormula != "" && binary != "" {
		if resolved := resolveAgentBin(binary, cwd); resolved != "" {
			real := resolved
			if r, err := filepath.EvalSymlinks(resolved); err == nil {
				real = r
			}
			if isHomebrewInstallPath(resolved) || isHomebrewInstallPath(real) {
				if brewPath := resolveAgentBin("brew", cwd); brewPath != "" {
					// A third-party formula ("user/repo/name") 404s from brew
					// until its tap is added — the binary being installed
					// already proves the tap was added once, but `brew
					// upgrade` doesn't implicitly re-add it (e.g. after
					// `brew untap` or a fresh machine restoring dotfiles
					// without Brewfile taps). `brew tap` is a no-op if it's
					// already there, so this is safe to run unconditionally.
					if tap, ok := homebrewTapFor(homebrewFormula); ok {
						tapCtx, tapCancel := context.WithTimeout(context.Background(), updateTimeout)
						exec.CommandContext(tapCtx, brewPath, "tap", tap).Run()
						tapCancel()
					}
					exe, args = brewPath, []string{"upgrade", homebrewFormula}
				}
			}
		}
	}

	if exe == "" {
		if pkg == "" {
			return ProviderUpdateResult{Error: "no package configured"}
		}
		npmPath := resolveAgentBin("npm", cwd)
		if npmPath == "" {
			return ProviderUpdateResult{Error: "npm not found on PATH"}
		}
		exe, args = npmPath, []string{"install", "-g", pkg + "@latest"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, exe, args...).CombinedOutput()
	if ctx.Err() != nil {
		return ProviderUpdateResult{Error: "update timed out"}
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return ProviderUpdateResult{Error: msg}
	}
	return ProviderUpdateResult{Ok: true}
}
