package main

import (
	"fmt"
	"strings"

	"burrow/internal/forge"
)

// ForgeStatus is what the PR panel's empty state and the Settings row both
// render from: which provider this repo is on, whether its CLI is there, and
// the exact commands to fix it if not. One call, so no UI layer hardcodes a
// provider's command names.
type ForgeStatus struct {
	Provider   string `json:"provider"` // "" when detection found nothing
	Installed  bool   `json:"installed"`
	Authed     bool   `json:"authed"`
	Bin        string `json:"bin,omitempty"`
	InstallCmd string `json:"installCmd,omitempty"`
	AuthCmd    string `json:"authCmd,omitempty"`
	DocsURL    string `json:"docsUrl,omitempty"`
}

// forgeRun adapts the app's runCmd to the package's Runner, resolving the CLI
// through resolveAgentBin first — the same PATH widening every other spawned
// binary gets, because a GUI app's PATH is not a login shell's.
func forgeRun(bin, cwd string, args []string) (string, string, int) {
	if resolved := resolveAgentBin(bin, cwd); resolved != "" {
		bin = resolved
	}
	out := runCmd(bin, cwd, args)
	return out.Stdout, out.Stderr, out.Code
}

// providerOverride reads the per-repo choice, climbing parent_id so a worktree
// inherits the root repo's provider — the same climb the Manager uses to keep
// one thread per root repo. Returns "" when no ancestor has one.
func (a *App) providerOverride(cwd string) string {
	var id int64
	if err := a.db.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, cwd).Scan(&id); err != nil {
		return ""
	}
	// Bounded so a parent_id cycle written by hand cannot spin forever.
	for i := 0; i < 16 && id != 0; i++ {
		var provider *string
		var parent *int64
		if err := a.db.QueryRow(`SELECT forge_provider, parent_id FROM workspaces WHERE id = ?`, id).Scan(&provider, &parent); err != nil {
			return ""
		}
		if provider != nil && *provider != "" {
			return *provider
		}
		if parent == nil {
			return ""
		}
		id = *parent
	}
	return ""
}

// detectProvider is remote URL first, stored override second. The URL wins
// because it is always current: a repo whose remote moved to a different host
// should follow it without the user remembering to clear a setting.
func (a *App) detectProvider(cwd string) forge.Provider {
	out := runCmd("git", cwd, []string{"remote", "get-url", "origin"})
	if out.Success {
		if p := forge.Detect(strings.TrimSpace(out.Stdout)); p != "" {
			return p
		}
	}
	return forge.Provider(a.providerOverride(cwd))
}

func (a *App) resolveForge(cwd string) (forge.Forge, error) {
	p := a.detectProvider(cwd)
	if p == "" {
		return nil, fmt.Errorf("no git forge detected for %s — pick one in the pull request panel", cwd)
	}
	return forge.New(p, forgeRun)
}

func (a *App) ForgeInfo(cwd string) ForgeStatus {
	p := a.detectProvider(cwd)
	if p == "" {
		return ForgeStatus{}
	}
	f, err := forge.New(p, forgeRun)
	if err != nil {
		return ForgeStatus{Provider: string(p)}
	}
	cli := f.CLI()
	st := ForgeStatus{
		Provider:   string(p),
		Bin:        cli.Bin,
		InstallCmd: cli.InstallCmd,
		AuthCmd:    cli.AuthCmd,
		DocsURL:    cli.DocsURL,
	}
	st.Installed = resolveAgentBin(cli.Bin, cwd) != ""
	if st.Installed && len(cli.AuthArgs) > 0 {
		_, _, code := forgeRun(cli.Bin, cwd, cli.AuthArgs)
		st.Authed = code == 0
	}
	return st
}

func (a *App) ForgePrList(cwd, scope, state string) ([]forge.PullRequest, error) {
	f, err := a.resolveForge(cwd)
	if err != nil {
		return nil, err
	}
	prs, err := f.List(cwd, forge.ListOpts{Scope: scope, State: state})
	if err != nil {
		return nil, err
	}
	// Marshal as [] rather than null: clients index the result without a guard.
	if prs == nil {
		prs = []forge.PullRequest{}
	}
	return prs, nil
}

func (a *App) ForgePrView(cwd string, number int) (forge.PullRequest, error) {
	f, err := a.resolveForge(cwd)
	if err != nil {
		return forge.PullRequest{}, err
	}
	return f.View(cwd, number)
}

func (a *App) ForgePrCreate(cwd, title, body, base, head string) (forge.PullRequest, error) {
	f, err := a.resolveForge(cwd)
	if err != nil {
		return forge.PullRequest{}, err
	}
	return f.Create(cwd, forge.CreateOpts{Title: title, Body: body, Base: base, Head: head})
}

func (a *App) ForgePrMerge(cwd string, number int, squash bool) error {
	f, err := a.resolveForge(cwd)
	if err != nil {
		return err
	}
	return f.Merge(cwd, number, squash)
}

// SetForgeProvider records the user's choice for a repo whose remote host says
// nothing — a self-hosted GitLab, say. An empty provider clears it and hands
// detection back to the remote URL.
func (a *App) SetForgeProvider(wsID int64, provider string) error {
	if provider != "" {
		if _, err := forge.New(forge.Provider(provider), forgeRun); err != nil {
			return err
		}
	}
	var v any
	if provider != "" {
		v = provider
	}
	if _, err := a.db.Exec(`UPDATE workspaces SET forge_provider = ? WHERE id = ?`, v, wsID); err != nil {
		return err
	}
	emitWorkspacesChanged()
	return nil
}
