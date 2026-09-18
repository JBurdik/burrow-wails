# Multi-forge pull request support

**Date:** 2026-09-18
**Status:** approved design, not yet implemented

## Problem

Burrow's pull request surface is hardwired to GitHub's `gh` CLI. `gh` leaks
through five layers:

| Layer | File | What it does |
|---|---|---|
| Backend passthrough | `src-wails/git.go:57` | `RunGh` — runs the `gh` binary with arbitrary argv |
| Wire table | `src-wails/remoteapi.go:214` | `run_gh` exposed at `scopeOrchOperate` |
| PR panel | `src/composables/usePullRequests.ts` | parses `gh pr list/view --json`, gh-shaped fields |
| Sidebar badge | `src/stores/git.ts:161` | `gh pr view --json` sweep every 60 s |
| Control verbs | `src-wails/internal/control/verbs_vcs.go` | `pr_create` / `pr_list` / `pr_view` / `pr_merge` → `c.deps.Gh.Run` |

A user on GitLab, Azure DevOps or Gitea gets no PR panel, no sidebar badge, and
four MCP verbs that fail. The control verbs are the worst of it: they are the
same surface the Manager, `burrow` CLI and MCP all reach, so "Burrow can open a
PR" is false for anyone off GitHub.

## Scope

**In:** GitHub (existing), GitLab (`glab`), Azure DevOps (`az repos`),
Gitea/Forgejo (`tea`).

**Out, deliberately:** Bitbucket — no official CLI exists, so it would need a
REST client with its own PAT storage, breaking the "CLI owns auth" model that
makes everything else cheap. The `Forge` interface leaves the door open.

Also out: review/approve actions beyond today's `act`, a per-provider
capability registry, writing PR comments.

## Approach: a `Forge` interface in Go

Three options were considered.

**A. `Forge` interface in Go** — chosen. One interface, four implementations,
one normalized `PullRequest` struct. Frontend, MCP and mobile all receive the
same shape.

**B. Declarative table** — `map[provider]spec{listArgs, fieldMap}` plus a
generic runner. Fewer lines for `glab`/`tea`, but Azure DevOps uses
organization/project/repository instead of owner/repo and expresses merge as
`--status completed`. The generic runner would grow `if provider == "azure"`
branches inside itself, which is A with worse boundaries.

**C. Normalize in the frontend** — rejected. The project already settled this
argument for the provider protocol: the parse belongs to whoever owns the
process. Frontend normalization would force the mobile client to reimplement
it, the way the chat transcript once had three partial parsers.

**What makes A cheap: every CLI has an `api` escape hatch.** `gh api`,
`glab api` and `tea api` issue raw REST calls under the CLI's own auth. A field
the porcelain command does not expose is reachable without us implementing an
API client, storing a token, or handling a refresh. We fill gaps, not build a
second transport.

## Package: `src-wails/internal/forge`

Must not import `main` — same rule as `internal/agentphase`. IO arrives as an
injected runner, so tests need neither a network nor an installed CLI.

```go
type Provider string // "github" | "gitlab" | "azure" | "gitea"

type Runner func(bin, cwd string, args []string) (stdout, stderr string, code int)

type CLIInfo struct {
    Bin         string // "glab"
    InstallCmd  string // "brew install glab"
    AuthCmd     string // "glab auth login"
    DocsURL     string // shown instead of InstallCmd off macOS
}

type PullRequest struct {
    Number    int       `json:"number"`
    Title     string    `json:"title"`
    Body      string    `json:"body,omitempty"`
    URL       string    `json:"url"`
    State     string    `json:"state"`   // normalized: open | merged | closed
    IsDraft   bool      `json:"isDraft"`
    HeadRef   string    `json:"headRefName"`
    BaseRef   string    `json:"baseRefName"`
    Author    string    `json:"author,omitempty"`
    UpdatedAt string    `json:"updatedAt,omitempty"`
    Checks    []Check   `json:"checks,omitempty"`
    Reviews   string    `json:"reviewDecision,omitempty"`
    Files     []File    `json:"files,omitempty"`
    Comments  []Comment `json:"comments,omitempty"`
}

type Forge interface {
    Provider() Provider
    CLI() CLIInfo
    List(cwd string, o ListOpts) ([]PullRequest, error)
    View(cwd string, number int) (PullRequest, error)   // number 0 = PR for the current branch
    Create(cwd string, o CreateOpts) (PullRequest, error)
    Merge(cwd string, number int, squash bool) error
}
```

**Optional fields are the capability model.** Azure DevOps has no
`statusCheckRollup` equivalent, so `Checks` stays empty and the panel hides that
section with a `v-if`. No feature flags, no capability registry — a missing
field is the signal.

**State normalization** happens in each adapter: `az` reports `completed` /
`abandoned`, `glab` reports `merged` / `closed`, and callers see one vocabulary.

## Detection

`forge/detect.go`, a pure function over a remote URL so it is testable without a
repo:

1. `git remote get-url origin` → host
2. Host match: `github.com` → github; `gitlab.com` → gitlab;
   `dev.azure.com`, `*.visualstudio.com` → azure; `codeberg.org` → gitea
3. Otherwise: the per-repo override from SQLite
4. Otherwise: unknown — the panel offers a provider picker

Self-hosted instances are why steps 3 and 4 exist: a GitLab at `git.firma.cz` is
indistinguishable from anything else by hostname alone.

**Override storage:** `ALTER TABLE workspaces ADD COLUMN forge_provider TEXT`,
following the existing migration pattern in `db.go:138-142`. Lookup climbs
`parent_id` to the root repo, so a worktree inherits the repo's choice — the
same climb the Manager uses to keep one thread per root repo. Set once, applies
to every worktree of that repo.

**Azure DevOps needs organization and project**, both parsed from the remote URL
(`dev.azure.com/{org}/{project}/_git/{repo}`, and the older
`{org}.visualstudio.com/{project}/_git/{repo}`). The user configures nothing. A
URL that does not parse produces an error naming what was missing, not a silent
failure.

## Wire surface

New `App` methods in `src-wails/forge.go`, a thin shell over the package:

| Wire command | Method | Scope |
|---|---|---|
| `forge_info` | `ForgeInfo(cwd)` | `scopeOrchRead` |
| `forge_pr_list` | `ForgePrList(cwd, scope, state)` | `scopeOrchRead` |
| `forge_pr_view` | `ForgePrView(cwd, number)` | `scopeOrchRead` |
| `forge_pr_create` | `ForgePrCreate(cwd, title, body, base)` | `scopeOrchOperate` |
| `forge_pr_merge` | `ForgePrMerge(cwd, number, squash)` | `scopeOrchOperate` |
| `set_forge_provider` | `SetForgeProvider(wsId, provider)` | `scopeOrchOperate` |

`ForgeInfo` returns the detected provider, whether its CLI is installed, whether
it is authenticated, and its `CLIInfo` — one call feeds both the panel's empty
state and the Settings row.

Both guard tests cover this for free: `TestRemoteSurfaceIsExhaustive` rejects a
new `App` method that is in neither list, and `commandSurface.test.ts` rejects an
`invoke("forge_…")` with no table entry.

**`run_gh` is removed** — from the wire table and from `git.go`. Once the call
sites migrate nothing calls it, and leaving it in place keeps a second,
GitHub-only path to the same capability that a later feature would reach for by
accident. `run_git` stays; it is provider-agnostic.

## Call site migration

1. **`usePullRequests.ts`** — the `gh()` helper, `listFields` and `detailFields`
   all go; field selection belongs to the adapter now. Scope filter, search and
   tabs are unchanged, only the `invoke` name and the absence of a JSON parse.
2. **`stores/git.ts:161`** — the badge sweep calls `forge_pr_view` with no
   number, meaning "the PR for the current branch". The `PR_POOL = 3`
   concurrency cap and the `prInFlight` dedupe stay exactly as they are.
3. **`verbs_vcs.go`** — `pr_create` / `pr_list` / `pr_view` / `pr_merge` keep
   their names and argument shapes. They are a public contract: the Manager
   primer, the MCP tool schemas and `burrow help` are all generated from the
   registry, and renaming would break every agent that learned them. Only the
   body changes (`c.deps.Gh.Run` → `c.deps.Forge`) and the summary stops saying
   "with the gh CLI".
4. **`control.go` `Deps`** — `Gh Runner` becomes `Forge ForgeClient`.
5. **`PullRequestsPanel.vue:48` and `CommitPushMenu.vue:56`** — the hardcoded
   `gh auth login` and `gh pr create --fill` strings come from `forge_info`'s
   `CLIInfo` instead.

`generate_pr_content` is untouched: it reads the branch's commits and diff and
knows nothing about the forge.

## Settings → Integrations: CLI installer

One row per provider in the existing Integrations section
(`Settings.vue:390`): icon, state, button.

State comes from `ForgeInfo` — installed and authenticated, installed but not
authenticated, or missing. Detection reuses `resolveAgentBin()` (`git.go:59`)
plus the CLI's own `auth status`.

**The button opens a new Burrow tab running the command** rather than installing
silently. The output is visible, Homebrew can prompt, and the user can kill it.
No install engine, no binary downloads, no sudo — a pre-filled command in a tab.

| Provider | Install | Auth |
|---|---|---|
| GitLab | `brew install glab` | `glab auth login` |
| Azure DevOps | `brew install azure-cli && az extension add --name azure-devops` | `az login` |
| Gitea | `brew install tea` | `tea login add` |

**Off macOS** there is no `brew`, so the button shows the provider's docs URL
instead of a command. An `apt`/`winget`/`choco` matrix is YAGNI for a
macOS-first app.

## Testing

- `forge/detect_test.go` — table test from remote URL to provider: HTTPS and SSH
  forms (`git@gitlab.com:group/repo.git`), both Azure shapes, and a self-hosted
  host resolving to empty so the override path is exercised.
- `forge/{github,gitlab,azure,gitea}_test.go` — each adapter gets captured real
  CLI JSON as a fixture and asserts the mapping onto `PullRequest`, including
  state normalization and absent optional fields. The injected runner means no
  network and no installed CLI in CI.
- `forge/azure_test.go` additionally covers organization/project extraction and
  the error message when the URL does not parse.

No end-to-end tests against live forges. The mapping is the entire risk surface
and a fixture covers it.

## Documentation to update

- `CLAUDE.md` — the backend file table gains `forge.go` / `internal/forge`; the
  `pr_*` verb description stops naming `gh`.
- `docs/context.html` — the Go bindings list.
- `docs/burrow.html` — the verb registry table, where `pr_create` is described
  as using the gh CLI.
