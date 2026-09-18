# Multi-forge Pull Request Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Burrow's pull request surface work on GitLab, Azure DevOps and Gitea in addition to GitHub, by routing every PR operation through a `Forge` interface backed by each provider's own CLI.

**Architecture:** A new `src-wails/internal/forge` package owns one `Forge` interface, one normalized `PullRequest` struct, and four CLI-backed implementations (`gh`, `glab`, `az repos`, `tea`). The package takes an injected `Runner` and never imports `main`, so every adapter is tested against captured JSON with no network and no installed CLI. A thin `src-wails/forge.go` exposes it as `App` methods on the `/v2/ws` command table; the frontend, the MCP verbs and the mobile client all consume the same normalized shape.

**Tech Stack:** Go 1.22+ (stdlib `encoding/json`, `os/exec` via the existing `runCmd`), SQLite (`ALTER TABLE` migration in `db.go`), Vue 3 + Pinia + TypeScript, vitest, `go test`.

**Spec:** `docs/superpowers/specs/2026-09-18-multi-forge-pr-design.md`

## Global Constraints

- `internal/forge` MUST NOT import `main`. Same rule as `internal/agentphase`: IO arrives as an injected `Runner`. A test that needs a network or an installed CLI is a failed test design.
- Providers in scope: `github`, `gitlab`, `azure`, `gitea`. Bitbucket is explicitly out.
- Optional fields ARE the capability model. A provider that cannot supply `Checks`, `Reviews`, `Files` or `Comments` leaves them empty; the UI hides the section with `v-if`. Do not add feature flags or a capability registry.
- `State` is normalized to exactly `open`, `merged` or `closed` by every adapter.
- The control verbs `pr_create`, `pr_list`, `pr_view`, `pr_merge` KEEP their names and argument shapes. They are a public contract — the Manager primer, MCP tool schemas and `burrow help` are generated from the registry.
- `run_gh` and `App.RunGh` are removed once call sites migrate. `run_git` stays.
- Adapter JSON fixtures come from each provider's documented REST schema, because each CLI passes its API's objects through verbatim: `glab` emits GitLab's Merge Request object, `az repos` emits Azure DevOps' `GitPullRequest`, `tea` emits Gitea's `PullRequest`.
- Go tests: `cd src-wails && go test ./...`. Frontend tests: `pnpm test`. Type check: `pnpm build`.
- Commit after every task. Commit messages in English regardless of chat language.

---

### Task 1: Forge package core — types, interface, detection

**Files:**
- Create: `src-wails/internal/forge/forge.go`
- Create: `src-wails/internal/forge/detect.go`
- Test: `src-wails/internal/forge/detect_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: every type below. Tasks 2–5 implement `Forge`; Task 6 calls `Detect`, `ParseAzureRemote` and `New`.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/forge/detect_test.go`:

```go
package forge

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		url  string
		want Provider
	}{
		{"https://github.com/JBurdik/burrow-wails.git", GitHub},
		{"git@github.com:JBurdik/burrow-wails.git", GitHub},
		{"https://gitlab.com/group/sub/repo.git", GitLab},
		{"git@gitlab.com:group/sub/repo.git", GitLab},
		{"https://dev.azure.com/acme/Platform/_git/api", Azure},
		{"git@ssh.dev.azure.com:v3/acme/Platform/api", Azure},
		{"https://acme.visualstudio.com/Platform/_git/api", Azure},
		{"https://codeberg.org/user/repo.git", Gitea},
		{"https://git.firma.cz/team/repo.git", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Detect(c.url); got != c.want {
			t.Errorf("Detect(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestParseAzureRemote(t *testing.T) {
	cases := []struct {
		url                   string
		org, project, repo    string
		wantErr               bool
	}{
		{"https://dev.azure.com/acme/Platform/_git/api", "acme", "Platform", "api", false},
		{"git@ssh.dev.azure.com:v3/acme/Platform/api", "acme", "Platform", "api", false},
		{"https://acme.visualstudio.com/Platform/_git/api", "acme", "Platform", "api", false},
		{"https://dev.azure.com/acme", "", "", "", true},
	}
	for _, c := range cases {
		org, project, repo, err := ParseAzureRemote(c.url)
		if (err != nil) != c.wantErr {
			t.Fatalf("ParseAzureRemote(%q) err = %v, wantErr %v", c.url, err, c.wantErr)
		}
		if err != nil {
			continue
		}
		if org != c.org || project != c.project || repo != c.repo {
			t.Errorf("ParseAzureRemote(%q) = %q/%q/%q, want %q/%q/%q",
				c.url, org, project, repo, c.org, c.project, c.repo)
		}
	}
}

func TestNormState(t *testing.T) {
	cases := map[string]string{
		"OPEN": "open", "opened": "open", "active": "open",
		"MERGED": "merged", "merged": "merged", "completed": "merged",
		"CLOSED": "closed", "closed": "closed", "abandoned": "closed",
		"": "",
	}
	for in, want := range cases {
		if got := normState(in); got != want {
			t.Errorf("normState(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/forge/...`
Expected: FAIL — the package does not compile, `undefined: Detect`.

- [ ] **Step 3: Write the types**

Create `src-wails/internal/forge/forge.go`:

```go
// Package forge normalizes pull requests across git hosting providers.
//
// Every provider is reached through the CLI the user already installs and
// authenticates themselves (gh, glab, az, tea): auth, self-hosted URLs and
// token refresh are the CLI's problem, not ours. What this package owns is the
// mapping from each CLI's JSON onto one PullRequest shape, so the panel, the
// MCP verbs and the phone all read the same fields.
//
// Like internal/agentphase, this package must not import main: IO arrives as an
// injected Runner, which is what lets every adapter be tested against captured
// JSON with no network and no installed CLI.
package forge

import (
	"fmt"
	"strconv"
)

type Provider string

const (
	GitHub Provider = "github"
	GitLab Provider = "gitlab"
	Azure  Provider = "azure"
	Gitea  Provider = "gitea"
)

// Runner runs a CLI in a working directory. The app injects its existing
// runCmd; tests inject a stub that asserts on argv and returns a fixture.
type Runner func(bin, cwd string, args []string) (stdout, stderr string, code int)

// CLIInfo is everything a caller needs to tell the user what is missing and how
// to fix it, without any UI layer hardcoding a provider's command names.
type CLIInfo struct {
	Bin        string `json:"bin"`
	InstallCmd string `json:"installCmd"`
	AuthCmd    string `json:"authCmd"`
	// AuthArgs is the subcommand that exits non-zero when not logged in.
	AuthArgs []string `json:"-"`
	DocsURL  string   `json:"docsUrl"`
}

type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
}

type File struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type Comment struct {
	Author    string `json:"author,omitempty"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// PullRequest is the normalized shape every adapter produces. Fields a
// provider cannot supply stay empty — that absence IS the capability model,
// and the UI hides the corresponding section rather than consulting a flag.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	URL       string    `json:"url"`
	State     string    `json:"state"` // open | merged | closed
	IsDraft   bool      `json:"isDraft"`
	HeadRef   string    `json:"headRefName"`
	BaseRef   string    `json:"baseRefName"`
	Author    string    `json:"author,omitempty"`
	UpdatedAt string    `json:"updatedAt,omitempty"`
	Additions int       `json:"additions,omitempty"`
	Deletions int       `json:"deletions,omitempty"`
	Checks    []Check   `json:"checks,omitempty"`
	Reviews   string    `json:"reviewDecision,omitempty"`
	Files     []File    `json:"files,omitempty"`
	Comments  []Comment `json:"comments,omitempty"`
}

// ListOpts.Scope is "assigned", "created" or "" for everything.
type ListOpts struct {
	Scope string
	State string // open | closed | merged | all; "" means open
}

type CreateOpts struct {
	Title string
	Body  string
	Base  string
	Head  string
}

type Forge interface {
	Provider() Provider
	CLI() CLIInfo
	// View with number 0 means "the pull request for the branch checked out in
	// cwd" — the sidebar badge asks that question every 60s and has no number.
	List(cwd string, o ListOpts) ([]PullRequest, error)
	View(cwd string, number int) (PullRequest, error)
	Create(cwd string, o CreateOpts) (PullRequest, error)
	Merge(cwd string, number int, squash bool) error
}

// New is declared in github.go and grows one case per adapter as Tasks 2-5
// land, so this package always compiles: a factory listing four types before
// any of them exists would not.

func itoa(n int) string { return strconv.Itoa(n) }

// cmdErr turns a non-zero CLI exit into an error carrying the CLI's own
// stderr. The CLI is the one that knows whether this is "not logged in", "no
// pull request for this branch" or "not a repository" — restating it here in
// our own words would only lose detail.
func cmdErr(bin string, args []string, stderr string, code int) error {
	msg := stderr
	if msg == "" {
		msg = fmt.Sprintf("exit %d", code)
	}
	return fmt.Errorf("%s %v: %s", bin, args, msg)
}
```

- [ ] **Step 4: Write detection**

Create `src-wails/internal/forge/detect.go`:

```go
package forge

import (
	"fmt"
	"strings"
)

// Detect maps a git remote URL onto a provider, or "" when the host says
// nothing. A self-hosted GitLab at git.firma.cz is indistinguishable from
// anything else by hostname, which is why the caller keeps a per-repo override
// and a picker for the "" case.
func Detect(remoteURL string) Provider {
	host := remoteHost(remoteURL)
	switch {
	case host == "":
		return ""
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return GitHub
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return GitLab
	case host == "dev.azure.com" || host == "ssh.dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com"):
		return Azure
	case host == "codeberg.org":
		return Gitea
	}
	return ""
}

// remoteHost pulls the host out of both URL forms git uses: an scp-style
// "git@host:path" and a real URL "scheme://[user@]host/path".
func remoteHost(remoteURL string) string {
	s := strings.TrimSpace(remoteURL)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else if at := strings.Index(s, "@"); at >= 0 && strings.Index(s, ":") > at {
		// scp-style: git@host:path
		s = s[at+1:]
		if c := strings.Index(s, ":"); c >= 0 {
			return strings.ToLower(s[:c])
		}
		return strings.ToLower(s)
	}
	if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	if i := strings.IndexAny(s, "/:"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// ParseAzureRemote extracts the organization, project and repository the
// az CLI needs as explicit flags. Azure is the one provider whose CLI is not
// repo-aware from cwd alone, so this has to come out of the remote URL — the
// alternative is asking the user for three values they already committed.
//
// Three shapes exist in the wild:
//
//	https://dev.azure.com/{org}/{project}/_git/{repo}
//	git@ssh.dev.azure.com:v3/{org}/{project}/{repo}
//	https://{org}.visualstudio.com/{project}/_git/{repo}
func ParseAzureRemote(remoteURL string) (org, project, repo string, err error) {
	host := remoteHost(remoteURL)
	s := strings.TrimSuffix(strings.TrimSpace(remoteURL), ".git")

	if strings.HasSuffix(host, ".visualstudio.com") {
		org = strings.TrimSuffix(host, ".visualstudio.com")
		parts := pathSegments(s, host)
		// {project}/_git/{repo}
		if len(parts) >= 3 && parts[len(parts)-2] == "_git" {
			return org, parts[len(parts)-3], parts[len(parts)-1], nil
		}
		return "", "", "", fmt.Errorf("forge: cannot read project/repo out of Azure remote %q", remoteURL)
	}

	parts := pathSegments(s, host)
	// scp-style carries a leading "v3" segment.
	if len(parts) > 0 && parts[0] == "v3" {
		parts = parts[1:]
	}
	// Drop the "_git" marker so both shapes collapse to org/project/repo.
	filtered := parts[:0]
	for _, p := range parts {
		if p != "_git" && p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) < 3 {
		return "", "", "", fmt.Errorf("forge: cannot read org/project/repo out of Azure remote %q", remoteURL)
	}
	return filtered[0], filtered[1], filtered[2], nil
}

func pathSegments(remoteURL, host string) []string {
	s := remoteURL
	if i := strings.Index(s, host); i >= 0 {
		s = s[i+len(host):]
	}
	s = strings.TrimLeft(s, ":/")
	return strings.Split(s, "/")
}

// normState collapses each provider's vocabulary onto open|merged|closed.
// Azure says "completed"/"abandoned", GitLab says "opened", GitHub SHOUTS.
func normState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "open", "opened", "active":
		return "open"
	case "merged", "completed":
		return "merged"
	case "closed", "abandoned", "declined":
		return "closed"
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./internal/forge/...`
Expected: PASS — `TestDetect`, `TestParseAzureRemote`, `TestNormState`. The
package has types and detection but no adapters yet, which compiles fine
because nothing references an adapter type.

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/forge/
git commit -m "Add forge package types and provider detection"
```

---

### Task 2: GitHub adapter

**Files:**
- Create: `src-wails/internal/forge/github.go`
- Test: `src-wails/internal/forge/github_test.go`
- Modify: `src-wails/internal/forge/forge.go` (nothing, unless `itoa` is missing)

**Interfaces:**
- Consumes: `Runner`, `PullRequest`, `Check`, `File`, `Comment`, `ListOpts`, `CreateOpts`, `CLIInfo`, `normState`, `cmdErr` from Task 1.
- Produces: `githubForge` satisfying `Forge`. No other task calls it directly — they all go through `New`.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/forge/github_test.go`:

```go
package forge

import (
	"strings"
	"testing"
)

// stubRunner records the argv it was called with and replays canned output.
type stubRunner struct {
	calls  [][]string
	out    string
	stderr string
	code   int
}

func (s *stubRunner) run(bin, cwd string, args []string) (string, string, int) {
	s.calls = append(s.calls, append([]string{bin}, args...))
	return s.out, s.stderr, s.code
}

const ghListJSON = `[
  {"number":42,"title":"Add forge package","url":"https://github.com/a/b/pull/42",
   "state":"OPEN","isDraft":false,"headRefName":"feat/forge","updatedAt":"2026-09-18T10:00:00Z",
   "author":{"login":"jirka"}}
]`

func TestGithubList(t *testing.T) {
	s := &stubRunner{out: ghListJSON}
	f, err := New(GitHub, s.run)
	if err != nil {
		t.Fatal(err)
	}
	prs, err := f.List("/repo", ListOpts{Scope: "assigned"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	got := prs[0]
	if got.Number != 42 || got.Title != "Add forge package" || got.State != "open" {
		t.Errorf("bad mapping: %+v", got)
	}
	if got.Author != "jirka" || got.HeadRef != "feat/forge" {
		t.Errorf("bad mapping: %+v", got)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "pr list") || !strings.Contains(argv, "--assignee @me") {
		t.Errorf("unexpected argv: %s", argv)
	}
}

const ghViewJSON = `{"number":42,"title":"Add forge package","body":"does things",
 "url":"https://github.com/a/b/pull/42","state":"MERGED","isDraft":false,
 "headRefName":"feat/forge","baseRefName":"main","updatedAt":"2026-09-18T10:00:00Z",
 "additions":120,"deletions":8,"author":{"login":"jirka"},"reviewDecision":"APPROVED",
 "statusCheckRollup":[{"name":"build","status":"COMPLETED","conclusion":"SUCCESS"}],
 "files":[{"path":"src-wails/forge.go","additions":100,"deletions":0}],
 "comments":[{"author":{"login":"reviewer"},"body":"lgtm","createdAt":"2026-09-18T11:00:00Z"}]}`

func TestGithubView(t *testing.T) {
	s := &stubRunner{out: ghViewJSON}
	f, _ := New(GitHub, s.run)
	pr, err := f.View("/repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if len(pr.Checks) != 1 || pr.Checks[0].Name != "build" || pr.Checks[0].Conclusion != "SUCCESS" {
		t.Errorf("Checks = %+v", pr.Checks)
	}
	if len(pr.Files) != 1 || pr.Files[0].Path != "src-wails/forge.go" {
		t.Errorf("Files = %+v", pr.Files)
	}
	if len(pr.Comments) != 1 || pr.Comments[0].Author != "reviewer" {
		t.Errorf("Comments = %+v", pr.Comments)
	}
	if pr.Reviews != "APPROVED" || pr.Additions != 120 {
		t.Errorf("bad mapping: %+v", pr)
	}
}

// View with number 0 asks for the PR of the branch in cwd — no number in argv.
func TestGithubViewCurrentBranch(t *testing.T) {
	s := &stubRunner{out: ghViewJSON}
	f, _ := New(GitHub, s.run)
	if _, err := f.View("/repo", 0); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(s.calls[0], " ")
	if strings.Contains(argv, " 0 ") {
		t.Errorf("number 0 leaked into argv: %s", argv)
	}
}

func TestGithubErrorCarriesStderr(t *testing.T) {
	s := &stubRunner{stderr: "no pull requests found for branch", code: 1}
	f, _ := New(GitHub, s.run)
	_, err := f.View("/repo", 0)
	if err == nil || !strings.Contains(err.Error(), "no pull requests found") {
		t.Fatalf("err = %v, want the CLI's own stderr", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/forge/ -run TestGithub -v`
Expected: FAIL to build — `undefined: New`.

- [ ] **Step 3: Write the adapter**

Create `src-wails/internal/forge/github.go`:

```go
package forge

import "encoding/json"

type githubForge struct{ run Runner }

func (g *githubForge) Provider() Provider { return GitHub }

func (g *githubForge) CLI() CLIInfo {
	return CLIInfo{
		Bin:        "gh",
		InstallCmd: "brew install gh",
		AuthCmd:    "gh auth login",
		AuthArgs:   []string{"auth", "status"},
		DocsURL:    "https://cli.github.com",
	}
}

// gh's own JSON shapes. Kept unexported and local: the whole point of the
// package is that nothing outside it sees a provider's field names.
type ghUser struct {
	Login string `json:"login"`
}

type ghPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	URL         string `json:"url"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	UpdatedAt   string `json:"updatedAt"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	Author      *ghUser `json:"author"`
	ReviewDecision string `json:"reviewDecision"`
	StatusCheckRollup []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"statusCheckRollup"`
	Files []struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`
	Comments []struct {
		Author    *ghUser `json:"author"`
		Body      string  `json:"body"`
		CreatedAt string  `json:"createdAt"`
	} `json:"comments"`
}

func (p ghPR) normalize() PullRequest {
	out := PullRequest{
		Number: p.Number, Title: p.Title, Body: p.Body, URL: p.URL,
		State: normState(p.State), IsDraft: p.IsDraft,
		HeadRef: p.HeadRefName, BaseRef: p.BaseRefName, UpdatedAt: p.UpdatedAt,
		Additions: p.Additions, Deletions: p.Deletions, Reviews: p.ReviewDecision,
	}
	if p.Author != nil {
		out.Author = p.Author.Login
	}
	for _, c := range p.StatusCheckRollup {
		out.Checks = append(out.Checks, Check{Name: c.Name, Status: c.Status, Conclusion: c.Conclusion})
	}
	for _, f := range p.Files {
		out.Files = append(out.Files, File{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions})
	}
	for _, c := range p.Comments {
		cm := Comment{Body: c.Body, CreatedAt: c.CreatedAt}
		if c.Author != nil {
			cm.Author = c.Author.Login
		}
		out.Comments = append(out.Comments, cm)
	}
	return out
}

const ghListFields = "number,title,url,state,isDraft,author,headRefName,updatedAt"
const ghDetailFields = ghListFields + ",body,baseRefName,additions,deletions,statusCheckRollup,reviewDecision,comments,files"

func (g *githubForge) exec(cwd string, args []string) ([]byte, error) {
	stdout, stderr, code := g.run("gh", cwd, args)
	if code != 0 {
		return nil, cmdErr("gh", args, stderr, code)
	}
	return []byte(stdout), nil
}

func (g *githubForge) List(cwd string, o ListOpts) ([]PullRequest, error) {
	state := o.State
	if state == "" {
		state = "open"
	}
	args := []string{"pr", "list", "--state", state, "--json", ghListFields, "--limit", "100"}
	switch o.Scope {
	case "assigned":
		args = append(args, "--assignee", "@me")
	case "created":
		args = append(args, "--author", "@me")
	}
	raw, err := g.exec(cwd, args)
	if err != nil {
		return nil, err
	}
	var list []ghPR
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(list))
	for _, p := range list {
		out = append(out, p.normalize())
	}
	return out, nil
}

func (g *githubForge) View(cwd string, number int) (PullRequest, error) {
	args := []string{"pr", "view"}
	if number > 0 {
		args = append(args, itoa(number))
	}
	args = append(args, "--json", ghDetailFields)
	raw, err := g.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var p ghPR
	if err := json.Unmarshal(raw, &p); err != nil {
		return PullRequest{}, err
	}
	return p.normalize(), nil
}

func (g *githubForge) Create(cwd string, o CreateOpts) (PullRequest, error) {
	args := []string{"pr", "create", "--title", o.Title, "--body", o.Body}
	if o.Base != "" {
		args = append(args, "--base", o.Base)
	}
	if o.Head != "" {
		args = append(args, "--head", o.Head)
	}
	if _, err := g.exec(cwd, args); err != nil {
		return PullRequest{}, err
	}
	// gh prints the new PR's URL, not its JSON. Re-reading the current branch's
	// PR is one extra call and gives the caller the same full shape every other
	// method returns, instead of a second half-populated code path.
	return g.View(cwd, 0)
}

func (g *githubForge) Merge(cwd string, number int, squash bool) error {
	args := []string{"pr", "merge", itoa(number)}
	if squash {
		args = append(args, "--squash")
	}
	_, err := g.exec(cwd, args)
	return err
}
```

Append the factory to the same file. It grows one `case` per adapter as Tasks
3-5 land:

```go
// New builds the adapter for a provider. An unknown provider is an error
// rather than a nil Forge, so a caller cannot dereference its way to a panic.
func New(p Provider, run Runner) (Forge, error) {
	switch p {
	case GitHub:
		return &githubForge{run: run}, nil
	}
	return nil, fmt.Errorf("forge: unknown provider %q", p)
}
```

Add `"fmt"` to `github.go`'s imports. `itoa` came with Task 1's `forge.go`; if
it is missing, add `func itoa(n int) string { return strconv.Itoa(n) }` there.

- [ ] **Step 4: Verify the factory compiles**

Run: `cd src-wails && go build ./internal/forge/`
Expected: no output.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./internal/forge/ -v`
Expected: PASS — all `TestGithub*` plus Task 1's tests.

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/forge/
git commit -m "Add GitHub forge adapter"
```

---

### Task 3: GitLab adapter

**Files:**
- Create: `src-wails/internal/forge/gitlab.go`
- Test: `src-wails/internal/forge/gitlab_test.go`
- Modify: `src-wails/internal/forge/github.go` (add the `case GitLab:` arm to `New`)

**Interfaces:**
- Consumes: the Task 1 types, plus `stubRunner` from `github_test.go` (same package, so it is in scope).
- Produces: `gitlabForge` satisfying `Forge`.

GitLab calls them merge requests. `glab mr` is the command; the JSON is
GitLab's Merge Request API object, so `iid` (not `id`) is the user-visible
number, `source_branch`/`target_branch` are the refs, and `web_url` is the link.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/forge/gitlab_test.go`:

```go
package forge

import (
	"strings"
	"testing"
)

const glListJSON = `[
  {"iid":7,"title":"Add forge package","web_url":"https://gitlab.com/a/b/-/merge_requests/7",
   "state":"opened","draft":false,"source_branch":"feat/forge","target_branch":"main",
   "updated_at":"2026-09-18T10:00:00Z","author":{"username":"jirka"}}
]`

func TestGitlabList(t *testing.T) {
	s := &stubRunner{out: glListJSON}
	f, err := New(GitLab, s.run)
	if err != nil {
		t.Fatal(err)
	}
	prs, err := f.List("/repo", ListOpts{Scope: "assigned"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d MRs, want 1", len(prs))
	}
	got := prs[0]
	if got.Number != 7 || got.State != "open" || got.HeadRef != "feat/forge" {
		t.Errorf("bad mapping: %+v", got)
	}
	if got.URL != "https://gitlab.com/a/b/-/merge_requests/7" || got.Author != "jirka" {
		t.Errorf("bad mapping: %+v", got)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "glab mr list") || !strings.Contains(argv, "--assignee @me") {
		t.Errorf("unexpected argv: %s", argv)
	}
	if !strings.Contains(argv, "--output json") {
		t.Errorf("argv must ask for json: %s", argv)
	}
}

const glViewJSON = `{"iid":7,"title":"Add forge package","description":"does things",
 "web_url":"https://gitlab.com/a/b/-/merge_requests/7","state":"merged","draft":false,
 "source_branch":"feat/forge","target_branch":"main","updated_at":"2026-09-18T10:00:00Z",
 "author":{"username":"jirka"},
 "pipeline":{"status":"success"},
 "changes":[{"new_path":"src-wails/forge.go"}]}`

func TestGitlabView(t *testing.T) {
	s := &stubRunner{out: glViewJSON}
	f, _ := New(GitLab, s.run)
	pr, err := f.View("/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "merged" || pr.Body != "does things" {
		t.Errorf("bad mapping: %+v", pr)
	}
	// The pipeline is GitLab's equivalent of a check rollup — one entry.
	if len(pr.Checks) != 1 || pr.Checks[0].Name != "pipeline" || pr.Checks[0].Conclusion != "success" {
		t.Errorf("Checks = %+v", pr.Checks)
	}
	if len(pr.Files) != 1 || pr.Files[0].Path != "src-wails/forge.go" {
		t.Errorf("Files = %+v", pr.Files)
	}
}

// A merge request with no pipeline leaves Checks empty rather than inventing a
// pending entry — an absent field is how the UI learns to hide the section.
func TestGitlabViewNoPipeline(t *testing.T) {
	s := &stubRunner{out: `{"iid":7,"state":"opened","web_url":"u","source_branch":"a","target_branch":"b"}`}
	f, _ := New(GitLab, s.run)
	pr, _ := f.View("/repo", 7)
	if len(pr.Checks) != 0 {
		t.Errorf("Checks = %+v, want empty", pr.Checks)
	}
}

func TestGitlabMergeSquash(t *testing.T) {
	s := &stubRunner{out: ""}
	f, _ := New(GitLab, s.run)
	if err := f.Merge("/repo", 7, true); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "mr merge 7") || !strings.Contains(argv, "--squash") {
		t.Errorf("unexpected argv: %s", argv)
	}
	if !strings.Contains(argv, "--yes") {
		t.Errorf("merge must not wait on an interactive prompt: %s", argv)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/forge/ -run TestGitlab -v`
Expected: FAIL — `forge: unknown provider "gitlab"`.

- [ ] **Step 3: Write the adapter**

Create `src-wails/internal/forge/gitlab.go`:

```go
package forge

import "encoding/json"

type gitlabForge struct{ run Runner }

func (g *gitlabForge) Provider() Provider { return GitLab }

func (g *gitlabForge) CLI() CLIInfo {
	return CLIInfo{
		Bin:        "glab",
		InstallCmd: "brew install glab",
		AuthCmd:    "glab auth login",
		AuthArgs:   []string{"auth", "status"},
		DocsURL:    "https://gitlab.com/gitlab-org/cli",
	}
}

// GitLab's Merge Request object, which glab passes through verbatim. `iid` is
// the per-project number users see; `id` is a global row id and would show the
// wrong number in every UI that printed it.
type glMR struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	WebURL       string `json:"web_url"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	UpdatedAt    string `json:"updated_at"`
	Author       *struct {
		Username string `json:"username"`
	} `json:"author"`
	Pipeline *struct {
		Status string `json:"status"`
	} `json:"pipeline"`
	Changes []struct {
		NewPath string `json:"new_path"`
	} `json:"changes"`
}

func (m glMR) normalize() PullRequest {
	out := PullRequest{
		Number: m.IID, Title: m.Title, Body: m.Description, URL: m.WebURL,
		State: normState(m.State), IsDraft: m.Draft,
		HeadRef: m.SourceBranch, BaseRef: m.TargetBranch, UpdatedAt: m.UpdatedAt,
	}
	if m.Author != nil {
		out.Author = m.Author.Username
	}
	// GitLab has one pipeline per MR rather than a list of named checks, so it
	// normalizes to a single entry. Absent pipeline stays absent.
	if m.Pipeline != nil && m.Pipeline.Status != "" {
		out.Checks = []Check{{Name: "pipeline", Status: m.Pipeline.Status, Conclusion: m.Pipeline.Status}}
	}
	for _, c := range m.Changes {
		out.Files = append(out.Files, File{Path: c.NewPath})
	}
	return out
}

func (g *gitlabForge) exec(cwd string, args []string) ([]byte, error) {
	stdout, stderr, code := g.run("glab", cwd, args)
	if code != 0 {
		return nil, cmdErr("glab", args, stderr, code)
	}
	return []byte(stdout), nil
}

func (g *gitlabForge) List(cwd string, o ListOpts) ([]PullRequest, error) {
	args := []string{"mr", "list", "--output", "json", "--per-page", "100"}
	switch o.State {
	case "", "open":
		args = append(args, "--opened")
	case "merged":
		args = append(args, "--merged")
	case "closed":
		args = append(args, "--closed")
	case "all":
		args = append(args, "--all")
	}
	switch o.Scope {
	case "assigned":
		args = append(args, "--assignee", "@me")
	case "created":
		args = append(args, "--author", "@me")
	}
	raw, err := g.exec(cwd, args)
	if err != nil {
		return nil, err
	}
	var list []glMR
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(list))
	for _, m := range list {
		out = append(out, m.normalize())
	}
	return out, nil
}

func (g *gitlabForge) View(cwd string, number int) (PullRequest, error) {
	args := []string{"mr", "view"}
	if number > 0 {
		args = append(args, itoa(number))
	}
	args = append(args, "--output", "json")
	raw, err := g.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var m glMR
	if err := json.Unmarshal(raw, &m); err != nil {
		return PullRequest{}, err
	}
	return m.normalize(), nil
}

func (g *gitlabForge) Create(cwd string, o CreateOpts) (PullRequest, error) {
	args := []string{"mr", "create", "--title", o.Title, "--description", o.Body, "--yes"}
	if o.Base != "" {
		args = append(args, "--target-branch", o.Base)
	}
	if o.Head != "" {
		args = append(args, "--source-branch", o.Head)
	}
	if _, err := g.exec(cwd, args); err != nil {
		return PullRequest{}, err
	}
	return g.View(cwd, 0)
}

func (g *gitlabForge) Merge(cwd string, number int, squash bool) error {
	// --yes because a merge that blocks on a confirmation prompt would hang the
	// call: there is no tty behind a Wails binding or an MCP verb.
	args := []string{"mr", "merge", itoa(number), "--yes"}
	if squash {
		args = append(args, "--squash")
	}
	_, err := g.exec(cwd, args)
	return err
}
```

- [ ] **Step 4: Add the factory arm**

In `github.go`'s `New`, add above the `return nil, fmt.Errorf(...)`:

```go
	case GitLab:
		return &gitlabForge{run: run}, nil
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./internal/forge/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/forge/
git commit -m "Add GitLab forge adapter"
```

---

### Task 4: Azure DevOps adapter

**Files:**
- Create: `src-wails/internal/forge/azure.go`
- Test: `src-wails/internal/forge/azure_test.go`
- Modify: `src-wails/internal/forge/github.go` (add the `case Azure:` arm to `New`)

**Interfaces:**
- Consumes: Task 1's types plus `ParseAzureRemote`, and `stubRunner` from `github_test.go`.
- Produces: `azureForge` satisfying `Forge`.

Azure is the awkward one and this task is where that is paid for. `az repos` is
not repo-aware from cwd, so every call needs `--organization`, `--project` and
`--repository`, which come from the remote URL. The adapter therefore runs
`git remote get-url origin` itself through the same injected `Runner`, with
`"git"` as the binary — that is the one place a `Forge` shells out to git.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/forge/azure_test.go`:

```go
package forge

import (
	"strings"
	"testing"
)

// seqRunner replays a different answer per call, so a test can cover the
// adapter's `git remote get-url` call followed by its `az` call.
type seqRunner struct {
	calls [][]string
	outs  []string
	codes []int
}

func (s *seqRunner) run(bin, cwd string, args []string) (string, string, int) {
	i := len(s.calls)
	s.calls = append(s.calls, append([]string{bin}, args...))
	out, code := "", 0
	if i < len(s.outs) {
		out = s.outs[i]
	}
	if i < len(s.codes) {
		code = s.codes[i]
	}
	if code != 0 {
		return "", "az said no", code
	}
	return out, "", 0
}

const azListJSON = `[
  {"pullRequestId":11,"title":"Add forge package","description":"does things",
   "status":"active","isDraft":false,
   "sourceRefName":"refs/heads/feat/forge","targetRefName":"refs/heads/main",
   "creationDate":"2026-09-18T10:00:00Z",
   "createdBy":{"displayName":"Jirka"},
   "repository":{"webUrl":"https://dev.azure.com/acme/Platform/_git/api"}}
]`

func TestAzureListUsesRemoteDerivedFlags(t *testing.T) {
	s := &seqRunner{outs: []string{
		"https://dev.azure.com/acme/Platform/_git/api\n",
		azListJSON,
	}}
	f, err := New(Azure, s.run)
	if err != nil {
		t.Fatal(err)
	}
	prs, err := f.List("/repo", ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	got := prs[0]
	if got.Number != 11 || got.State != "open" {
		t.Errorf("bad mapping: %+v", got)
	}
	// refs/heads/ must be stripped — the UI shows a branch, not a ref path.
	if got.HeadRef != "feat/forge" || got.BaseRef != "main" {
		t.Errorf("refs not stripped: %+v", got)
	}
	if got.Author != "Jirka" {
		t.Errorf("Author = %q", got.Author)
	}
	// The PR URL is built from the repo web URL plus the id — Azure's JSON has
	// only an API url, which opens raw JSON in a browser.
	if got.URL != "https://dev.azure.com/acme/Platform/_git/api/pullrequest/11" {
		t.Errorf("URL = %q", got.URL)
	}
	if len(s.calls) != 2 {
		t.Fatalf("expected a git remote read then an az call, got %v", s.calls)
	}
	argv := strings.Join(s.calls[1], " ")
	for _, want := range []string{"az repos pr list", "--organization https://dev.azure.com/acme", "--project Platform", "--repository api", "--output json"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q: %s", want, argv)
		}
	}
}

func TestAzureStateNormalization(t *testing.T) {
	cases := map[string]string{"active": "open", "completed": "merged", "abandoned": "closed"}
	for in, want := range cases {
		s := &seqRunner{outs: []string{
			"https://dev.azure.com/acme/Platform/_git/api\n",
			`{"pullRequestId":11,"status":"` + in + `","sourceRefName":"refs/heads/a","targetRefName":"refs/heads/b","repository":{"webUrl":"u"}}`,
		}}
		f, _ := New(Azure, s.run)
		pr, err := f.View("/repo", 11)
		if err != nil {
			t.Fatal(err)
		}
		if pr.State != want {
			t.Errorf("status %q → State %q, want %q", in, pr.State, want)
		}
	}
}

// Azure supplies no check rollup in the PR object, so Checks stays empty and
// the panel hides that section. This is the capability model in action.
func TestAzureHasNoChecks(t *testing.T) {
	s := &seqRunner{outs: []string{
		"https://dev.azure.com/acme/Platform/_git/api\n",
		`{"pullRequestId":11,"status":"active","sourceRefName":"refs/heads/a","targetRefName":"refs/heads/b","repository":{"webUrl":"u"}}`,
	}}
	f, _ := New(Azure, s.run)
	pr, _ := f.View("/repo", 11)
	if len(pr.Checks) != 0 {
		t.Errorf("Checks = %+v, want empty", pr.Checks)
	}
}

func TestAzureMergeIsAnUpdateToCompleted(t *testing.T) {
	s := &seqRunner{outs: []string{"https://dev.azure.com/acme/Platform/_git/api\n", "{}"}}
	f, _ := New(Azure, s.run)
	if err := f.Merge("/repo", 11, true); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(s.calls[1], " ")
	for _, want := range []string{"az repos pr update", "--id 11", "--status completed", "--squash true"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q: %s", want, argv)
		}
	}
}

func TestAzureUnparseableRemoteIsAnActionableError(t *testing.T) {
	s := &seqRunner{outs: []string{"https://dev.azure.com/acme\n"}}
	f, _ := New(Azure, s.run)
	_, err := f.List("/repo", ListOpts{})
	if err == nil || !strings.Contains(err.Error(), "org/project/repo") {
		t.Fatalf("err = %v, want it to name what could not be read", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/forge/ -run TestAzure -v`
Expected: FAIL — `forge: unknown provider "azure"`.

- [ ] **Step 3: Write the adapter**

Create `src-wails/internal/forge/azure.go`:

```go
package forge

import (
	"encoding/json"
	"strings"
)

type azureForge struct{ run Runner }

func (a *azureForge) Provider() Provider { return Azure }

func (a *azureForge) CLI() CLIInfo {
	return CLIInfo{
		Bin: "az",
		// Two steps: the extension is where `az repos` actually lives, and a
		// bare azure-cli reports "repos is not a known command" — which reads
		// like a broken install rather than a missing extension.
		InstallCmd: "brew install azure-cli && az extension add --name azure-devops",
		AuthCmd:    "az login",
		AuthArgs:   []string{"account", "show"},
		DocsURL:    "https://learn.microsoft.com/cli/azure/repos/pr",
	}
}

// coords are the three values az needs on every call and cwd cannot supply.
type coords struct{ org, project, repo string }

func (a *azureForge) coords(cwd string) (coords, error) {
	stdout, stderr, code := a.run("git", cwd, []string{"remote", "get-url", "origin"})
	if code != 0 {
		return coords{}, cmdErr("git", []string{"remote", "get-url", "origin"}, stderr, code)
	}
	org, project, repo, err := ParseAzureRemote(strings.TrimSpace(stdout))
	if err != nil {
		return coords{}, err
	}
	return coords{org: org, project: project, repo: repo}, nil
}

func (c coords) flags() []string {
	return []string{
		"--organization", "https://dev.azure.com/" + c.org,
		"--project", c.project,
		"--repository", c.repo,
	}
}

// Azure's GitPullRequest object.
type azPR struct {
	ID            int    `json:"pullRequestId"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	IsDraft       bool   `json:"isDraft"`
	SourceRefName string `json:"sourceRefName"`
	TargetRefName string `json:"targetRefName"`
	CreationDate  string `json:"creationDate"`
	CreatedBy     *struct {
		DisplayName string `json:"displayName"`
	} `json:"createdBy"`
	Repository *struct {
		WebURL string `json:"webUrl"`
	} `json:"repository"`
}

func (p azPR) normalize() PullRequest {
	out := PullRequest{
		Number: p.ID, Title: p.Title, Body: p.Description,
		State: normState(p.Status), IsDraft: p.IsDraft,
		HeadRef: strings.TrimPrefix(p.SourceRefName, "refs/heads/"),
		BaseRef: strings.TrimPrefix(p.TargetRefName, "refs/heads/"),
		UpdatedAt: p.CreationDate,
	}
	if p.CreatedBy != nil {
		out.Author = p.CreatedBy.DisplayName
	}
	// Azure's JSON carries only an API url; the browsable one is the repo web
	// url plus /pullrequest/<id>. Handing the UI the API url would open raw
	// JSON in the user's browser.
	if p.Repository != nil && p.Repository.WebURL != "" {
		out.URL = strings.TrimSuffix(p.Repository.WebURL, "/") + "/pullrequest/" + itoa(p.ID)
	}
	return out
}

func (a *azureForge) exec(cwd string, args []string) ([]byte, error) {
	stdout, stderr, code := a.run("az", cwd, args)
	if code != 0 {
		return nil, cmdErr("az", args, stderr, code)
	}
	return []byte(stdout), nil
}

func (a *azureForge) List(cwd string, o ListOpts) ([]PullRequest, error) {
	c, err := a.coords(cwd)
	if err != nil {
		return nil, err
	}
	status := "active"
	switch o.State {
	case "merged":
		status = "completed"
	case "closed":
		status = "abandoned"
	case "all":
		status = "all"
	}
	args := append([]string{"repos", "pr", "list", "--status", status, "--output", "json"}, c.flags()...)
	switch o.Scope {
	case "assigned":
		args = append(args, "--reviewer", "@me")
	case "created":
		args = append(args, "--creator", "@me")
	}
	raw, err := a.exec(cwd, args)
	if err != nil {
		return nil, err
	}
	var list []azPR
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(list))
	for _, p := range list {
		out = append(out, p.normalize())
	}
	return out, nil
}

func (a *azureForge) View(cwd string, number int) (PullRequest, error) {
	c, err := a.coords(cwd)
	if err != nil {
		return PullRequest{}, err
	}
	if number <= 0 {
		// az has no "PR for the current branch" query, so list the active PRs
		// whose source ref is this branch and take the first.
		return a.currentBranchPR(cwd, c)
	}
	args := append([]string{"repos", "pr", "show", "--id", itoa(number), "--output", "json"}, c.flags()[:2]...)
	raw, err := a.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var p azPR
	if err := json.Unmarshal(raw, &p); err != nil {
		return PullRequest{}, err
	}
	return p.normalize(), nil
}

func (a *azureForge) currentBranchPR(cwd string, c coords) (PullRequest, error) {
	stdout, stderr, code := a.run("git", cwd, []string{"branch", "--show-current"})
	if code != 0 {
		return PullRequest{}, cmdErr("git", []string{"branch", "--show-current"}, stderr, code)
	}
	branch := strings.TrimSpace(stdout)
	args := append([]string{
		"repos", "pr", "list", "--status", "active", "--output", "json",
		"--source-branch", branch,
	}, c.flags()...)
	raw, err := a.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var list []azPR
	if err := json.Unmarshal(raw, &list); err != nil {
		return PullRequest{}, err
	}
	if len(list) == 0 {
		return PullRequest{}, cmdErr("az", args, "no pull request for branch "+branch, 1)
	}
	return list[0].normalize(), nil
}

func (a *azureForge) Create(cwd string, o CreateOpts) (PullRequest, error) {
	c, err := a.coords(cwd)
	if err != nil {
		return PullRequest{}, err
	}
	args := append([]string{
		"repos", "pr", "create", "--title", o.Title, "--description", o.Body, "--output", "json",
	}, c.flags()...)
	if o.Base != "" {
		args = append(args, "--target-branch", o.Base)
	}
	if o.Head != "" {
		args = append(args, "--source-branch", o.Head)
	}
	raw, err := a.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var p azPR
	if err := json.Unmarshal(raw, &p); err != nil {
		return PullRequest{}, err
	}
	return p.normalize(), nil
}

func (a *azureForge) Merge(cwd string, number int, squash bool) error {
	c, err := a.coords(cwd)
	if err != nil {
		return err
	}
	// Azure has no "merge" verb: completing a PR is an update of its status.
	args := append([]string{
		"repos", "pr", "update", "--id", itoa(number), "--status", "completed", "--output", "json",
	}, c.flags()[:2]...)
	if squash {
		args = append(args, "--squash", "true")
	}
	_, err = a.exec(cwd, args)
	return err
}
```

- [ ] **Step 4: Add the factory arm**

In `github.go`'s `New`, add above the `return nil, fmt.Errorf(...)`:

```go
	case Azure:
		return &azureForge{run: run}, nil
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd src-wails && go test ./internal/forge/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src-wails/internal/forge/
git commit -m "Add Azure DevOps forge adapter"
```

---

### Task 5: Gitea adapter

**Files:**
- Create: `src-wails/internal/forge/gitea.go`
- Test: `src-wails/internal/forge/gitea_test.go`
- Modify: `src-wails/internal/forge/github.go` (add the `case Gitea:` arm to `New`)

**Interfaces:**
- Consumes: Task 1's types plus `stubRunner` from `github_test.go`.
- Produces: `giteaForge` satisfying `Forge`.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/forge/gitea_test.go`:

```go
package forge

import (
	"strings"
	"testing"
)

const teaListJSON = `[
  {"number":3,"title":"Add forge package","body":"does things",
   "html_url":"https://codeberg.org/a/b/pulls/3","state":"open",
   "head":{"ref":"feat/forge"},"base":{"ref":"main"},
   "updated_at":"2026-09-18T10:00:00Z","user":{"login":"jirka"}}
]`

func TestGiteaList(t *testing.T) {
	s := &stubRunner{out: teaListJSON}
	f, err := New(Gitea, s.run)
	if err != nil {
		t.Fatal(err)
	}
	prs, err := f.List("/repo", ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1", len(prs))
	}
	got := prs[0]
	if got.Number != 3 || got.State != "open" || got.HeadRef != "feat/forge" || got.BaseRef != "main" {
		t.Errorf("bad mapping: %+v", got)
	}
	if got.URL != "https://codeberg.org/a/b/pulls/3" || got.Author != "jirka" {
		t.Errorf("bad mapping: %+v", got)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "tea pr list") || !strings.Contains(argv, "--output json") {
		t.Errorf("unexpected argv: %s", argv)
	}
}

// Gitea reports a merged PR as state "closed" plus merged:true — mapping on
// state alone would file every merged PR under "closed".
func TestGiteaMergedIsNotClosed(t *testing.T) {
	s := &stubRunner{out: `{"number":3,"state":"closed","merged":true,"html_url":"u","head":{"ref":"a"},"base":{"ref":"b"}}`}
	f, _ := New(Gitea, s.run)
	pr, err := f.View("/repo", 3)
	if err != nil {
		t.Fatal(err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
}

func TestGiteaMergeSquash(t *testing.T) {
	s := &stubRunner{out: ""}
	f, _ := New(Gitea, s.run)
	if err := f.Merge("/repo", 3, true); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "pr merge 3") || !strings.Contains(argv, "--style squash") {
		t.Errorf("unexpected argv: %s", argv)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/forge/ -run TestGitea -v`
Expected: FAIL — `forge: unknown provider "gitea"`.

- [ ] **Step 3: Write the adapter**

Create `src-wails/internal/forge/gitea.go`:

```go
package forge

import "encoding/json"

type giteaForge struct{ run Runner }

func (g *giteaForge) Provider() Provider { return Gitea }

func (g *giteaForge) CLI() CLIInfo {
	return CLIInfo{
		Bin:        "tea",
		InstallCmd: "brew install tea",
		AuthCmd:    "tea login add",
		AuthArgs:   []string{"login", "list"},
		DocsURL:    "https://gitea.com/gitea/tea",
	}
}

// Gitea's PullRequest object, which tea passes through.
type teaPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Draft   bool   `json:"draft"`
	Head    *struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base *struct {
		Ref string `json:"ref"`
	} `json:"base"`
	UpdatedAt string `json:"updated_at"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (p teaPR) normalize() PullRequest {
	state := normState(p.State)
	// Gitea reports a merged PR as closed with merged:true. Reading state
	// alone would file every merged PR under "closed".
	if p.Merged {
		state = "merged"
	}
	out := PullRequest{
		Number: p.Number, Title: p.Title, Body: p.Body, URL: p.HTMLURL,
		State: state, IsDraft: p.Draft, UpdatedAt: p.UpdatedAt,
	}
	if p.Head != nil {
		out.HeadRef = p.Head.Ref
	}
	if p.Base != nil {
		out.BaseRef = p.Base.Ref
	}
	if p.User != nil {
		out.Author = p.User.Login
	}
	return out
}

func (g *giteaForge) exec(cwd string, args []string) ([]byte, error) {
	stdout, stderr, code := g.run("tea", cwd, args)
	if code != 0 {
		return nil, cmdErr("tea", args, stderr, code)
	}
	return []byte(stdout), nil
}

func (g *giteaForge) List(cwd string, o ListOpts) ([]PullRequest, error) {
	state := o.State
	if state == "" {
		state = "open"
	}
	args := []string{"pr", "list", "--state", state, "--output", "json", "--limit", "100"}
	raw, err := g.exec(cwd, args)
	if err != nil {
		return nil, err
	}
	var list []teaPR
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(list))
	for _, p := range list {
		out = append(out, p.normalize())
	}
	return out, nil
}

func (g *giteaForge) View(cwd string, number int) (PullRequest, error) {
	args := []string{"pr"}
	if number > 0 {
		args = append(args, itoa(number))
	}
	args = append(args, "--output", "json")
	raw, err := g.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	var p teaPR
	if err := json.Unmarshal(raw, &p); err != nil {
		return PullRequest{}, err
	}
	return p.normalize(), nil
}

func (g *giteaForge) Create(cwd string, o CreateOpts) (PullRequest, error) {
	args := []string{"pr", "create", "--title", o.Title, "--description", o.Body}
	if o.Base != "" {
		args = append(args, "--base", o.Base)
	}
	if o.Head != "" {
		args = append(args, "--head", o.Head)
	}
	if _, err := g.exec(cwd, args); err != nil {
		return PullRequest{}, err
	}
	return g.View(cwd, 0)
}

func (g *giteaForge) Merge(cwd string, number int, squash bool) error {
	args := []string{"pr", "merge", itoa(number)}
	if squash {
		args = append(args, "--style", "squash")
	}
	_, err := g.exec(cwd, args)
	return err
}
```

- [ ] **Step 4: Add the factory arm**

In `github.go`'s `New`, add above the `return nil, fmt.Errorf(...)`:

```go
	case Gitea:
		return &giteaForge{run: run}, nil
```

`New` now lists all four providers and every adapter lives in its own file.

- [ ] **Step 5: Run the whole package**

Run: `cd src-wails && go test ./internal/forge/ -v`
Expected: PASS — every adapter test plus detection.

- [ ] **Step 6: Commit**

```bash
git add -A src-wails/internal/forge/
git commit -m "Add Gitea forge adapter"
```

---

### Task 6: App layer — provider resolution, bindings, wire table

**Files:**
- Create: `src-wails/forge.go`
- Test: `src-wails/forge_test.go`
- Modify: `src-wails/db.go:138-142` (add the migration line)
- Modify: `src-wails/workspace.go:17,25` (add the column to the struct and `workspaceCols`)
- Modify: `src-wails/remoteapi.go:205-215` (add six entries, remove `run_gh`)
- Modify: `src-wails/git.go:56-61` (delete `RunGh`)

**Interfaces:**
- Consumes: `forge.New`, `forge.Detect`, `forge.Provider`, `forge.PullRequest`, `forge.ListOpts`, `forge.CreateOpts`, `forge.CLIInfo` from Tasks 1–5; `runCmd` from `git.go:33`; `resolveAgentBin` from `claudechat.go:20`.
- Produces: `App.ForgeInfo(cwd string) ForgeStatus`, `App.ForgePrList(cwd, scope, state string) ([]forge.PullRequest, error)`, `App.ForgePrView(cwd string, number int) (forge.PullRequest, error)`, `App.ForgePrCreate(cwd, title, body, base, head string) (forge.PullRequest, error)`, `App.ForgePrMerge(cwd string, number int, squash bool) error`, `App.SetForgeProvider(wsID int64, provider string) error`, and `App.resolveForge(cwd string) (forge.Forge, error)` for Task 7.

- [ ] **Step 1: Write the failing test**

Create `src-wails/forge_test.go`. `newTestApp` is the existing helper used by
`workspace_test.go`; reuse it rather than opening a DB by hand.

```go
package main

import "testing"

// A worktree inherits its root repo's provider override: the override is set on
// the repo the user configured, and every worktree of it is the same forge.
func TestForgeProviderClimbsToRootRepo(t *testing.T) {
	a := newTestApp(t)
	repo, err := a.CreateWorkspace("repo", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	wt, err := a.CreateWorkspace("wt", "/tmp/repo-wt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE workspaces SET parent_id = ? WHERE id = ?`, repo.ID, wt.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetForgeProvider(repo.ID, "gitlab"); err != nil {
		t.Fatal(err)
	}
	if got := a.providerOverride("/tmp/repo-wt"); got != "gitlab" {
		t.Errorf("worktree override = %q, want gitlab", got)
	}
	if got := a.providerOverride("/tmp/repo"); got != "gitlab" {
		t.Errorf("repo override = %q, want gitlab", got)
	}
	if got := a.providerOverride("/tmp/unknown"); got != "" {
		t.Errorf("unknown path override = %q, want empty", got)
	}
}

func TestSetForgeProviderRejectsUnknown(t *testing.T) {
	a := newTestApp(t)
	ws, err := a.CreateWorkspace("repo", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetForgeProvider(ws.ID, "bitbucket"); err == nil {
		t.Fatal("expected an error for a provider with no adapter")
	}
	// Clearing is legal: it hands detection back to the remote URL.
	if err := a.SetForgeProvider(ws.ID, ""); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./ -run TestForge -v`
Expected: FAIL — `a.SetForgeProvider undefined`.

- [ ] **Step 3: Add the schema column**

In `src-wails/db.go`, add to the migration list that already contains the
`ALTER TABLE workspaces` lines at 138-142:

```go
		`ALTER TABLE workspaces ADD COLUMN forge_provider TEXT`,
```

In `src-wails/workspace.go`, add the field to the `Workspace` struct next to
`WorktreeBranch` (line 17):

```go
	ForgeProvider *string `json:"forge_provider,omitempty"`
```

and extend `workspaceCols` (line 25) with `, forge_provider`. Every `Scan` over
`workspaceCols` needs the extra destination — grep for `workspaceCols` and add
`&w.ForgeProvider` to each in the same position.

- [ ] **Step 4: Write the app layer**

Create `src-wails/forge.go`:

```go
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
```

Check the module path in `src-wails/go.mod` and use it in the import — the plan
writes `burrow/internal/forge`; if the module is named differently, match it.

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd src-wails && go test ./ -run TestForge -v`
Expected: PASS.

- [ ] **Step 6: Wire the commands and delete `run_gh`**

In `src-wails/remoteapi.go`, replace the `"run_gh"` line with the six new
entries and update the comment above the block so it stops describing `RunGh`:

```go
	"run_git":                 {Method: "RunGit", Args: []string{"cwd", "args"}, Scope: scopeOrchOperate},
	"forge_info":              {Method: "ForgeInfo", Args: []string{"cwd"}, Scope: scopeOrchRead},
	"forge_pr_list":           {Method: "ForgePrList", Args: []string{"cwd", "scope", "state"}, Scope: scopeOrchRead},
	"forge_pr_view":           {Method: "ForgePrView", Args: []string{"cwd", "number"}, Scope: scopeOrchRead},
	"forge_pr_create":         {Method: "ForgePrCreate", Args: []string{"cwd", "title", "body", "base", "head"}, Scope: scopeOrchOperate},
	"forge_pr_merge":          {Method: "ForgePrMerge", Args: []string{"cwd", "number", "squash"}, Scope: scopeOrchOperate},
	"set_forge_provider":      {Method: "SetForgeProvider", Args: []string{"wsId", "provider"}, Scope: scopeOrchOperate},
```

In `src-wails/git.go`, delete the whole `func (a *App) RunGh` (lines 56-61).

- [ ] **Step 7: Run the full backend suite**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS, including `TestRemoteSurfaceIsExhaustive` — it fails if any new
`App` method is in neither `remoteAllowed` nor `remoteDenied`, so a miss here is
caught now rather than at runtime.

Frontend call sites still reference `run_gh` and migrate in Task 8; the Go build
does not know about them, so this task's gate is the Go suite alone.

- [ ] **Step 8: Commit**

```bash
git add src-wails/forge.go src-wails/forge_test.go src-wails/db.go src-wails/workspace.go src-wails/remoteapi.go src-wails/git.go
git commit -m "Expose forge PR operations as App methods and drop run_gh"
```

---

### Task 7: Control verbs route through the forge

**Files:**
- Modify: `src-wails/internal/control/control.go:150-157` (swap `Gh` for `Forge` in `Deps`)
- Modify: `src-wails/internal/control/verbs_vcs.go:110-175` (four verb bodies, and `c.gh`)
- Modify: `src-wails/controlapi.go:44-48,185-187` (drop `ghRunner`, inject the forge client)
- Test: `src-wails/internal/control/verbs_vcs_test.go`

**Interfaces:**
- Consumes: `App.ForgePrList` / `ForgePrView` / `ForgePrCreate` / `ForgePrMerge` from Task 6; `forge.PullRequest`.
- Produces: `control.ForgeClient` interface, implemented in `controlapi.go` by `forgeClient{app}`.

The verb names and argument shapes MUST NOT change — the Manager primer, the MCP
tool schemas and `burrow help` are all generated from this registry, so a rename
silently breaks every agent that learned them.

- [ ] **Step 1: Write the failing test**

Create `src-wails/internal/control/verbs_vcs_test.go`:

```go
package control

import (
	"context"
	"testing"
)

type fakeForge struct {
	listCalls  []string // cwd of each List
	mergeCalls []int
	squash     []bool
}

func (f *fakeForge) List(cwd, scope, state string) (any, error) {
	f.listCalls = append(f.listCalls, cwd)
	return []map[string]any{{"number": 1, "title": "t", "state": state}}, nil
}
func (f *fakeForge) View(cwd string, number int) (any, error) {
	return map[string]any{"number": number}, nil
}
func (f *fakeForge) Create(cwd, title, body, base, head string) (any, error) {
	return map[string]any{"title": title, "baseRefName": base, "headRefName": head}, nil
}
func (f *fakeForge) Merge(cwd string, number int, squash bool) error {
	f.mergeCalls = append(f.mergeCalls, number)
	f.squash = append(f.squash, squash)
	return nil
}

func TestPrVerbsKeepTheirContract(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	for _, name := range []string{"pr_create", "pr_list", "pr_view", "pr_merge"} {
		if _, ok := c.verbs[name]; !ok {
			t.Fatalf("verb %q disappeared — it is a public contract", name)
		}
	}
}

func TestPrListGoesThroughTheForge(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	if _, err := c.Call(context.Background(), "pr_list", Params{"cwd": "/repo", "state": "open"}); err != nil {
		t.Fatal(err)
	}
	if len(ff.listCalls) != 1 || ff.listCalls[0] != "/repo" {
		t.Errorf("List calls = %v", ff.listCalls)
	}
}

func TestPrMergePassesSquash(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	if _, err := c.Call(context.Background(), "pr_merge", Params{"cwd": "/repo", "number": 5, "squash": true}); err != nil {
		t.Fatal(err)
	}
	if len(ff.mergeCalls) != 1 || ff.mergeCalls[0] != 5 || !ff.squash[0] {
		t.Errorf("Merge(%v, squash=%v)", ff.mergeCalls, ff.squash)
	}
}
```

Read `src-wails/internal/control/control.go` for the exact constructor and call
entry point names (`New`, `Call`) before writing this — if they differ, match
the real ones rather than these.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd src-wails && go test ./internal/control/ -run TestPr -v`
Expected: FAIL — `unknown field Forge in struct literal`.

- [ ] **Step 3: Swap the dependency**

In `control.go`, replace the `Gh CmdRunner` field in `Deps` with:

```go
	// Forge is the provider-neutral pull request client. It replaced a raw gh
	// CmdRunner: the verbs below are the same surface the Manager, the burrow
	// CLI and MCP all reach, so "Burrow can open a pull request" has to be true
	// off GitHub too.
	Forge ForgeClient
```

and add the interface next to `CmdRunner`:

```go
// ForgeClient is the pull request surface, injected so this package needs no
// knowledge of which CLI backs it. `any` rather than a concrete type keeps the
// forge package out of this one's imports — these values are marshalled
// straight to the caller.
type ForgeClient interface {
	List(cwd, scope, state string) (any, error)
	View(cwd string, number int) (any, error)
	Create(cwd, title, body, base, head string) (any, error)
	Merge(cwd string, number int, squash bool) error
}
```

- [ ] **Step 4: Rewrite the four verb bodies**

In `verbs_vcs.go`, delete the `func (c *Core) gh` helper and change the four
`Fn` bodies. Summaries stop naming gh:

```go
	}, {
		Name:    "pr_create",
		Summary: "Open a pull request (GitHub, GitLab, Azure DevOps or Gitea)",
		Args: []Arg{
			{Name: "title", Type: "string", Desc: "PR title", Required: true},
			{Name: "body", Type: "string", Desc: "PR body", Required: true},
			{Name: "base", Type: "string", Desc: "Base branch (default main)"},
			{Name: "head", Type: "string", Desc: "Head branch (default: the branch in cwd)"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			base := p.Str("base")
			if base == "" {
				base = "main"
			}
			return c.deps.Forge.Create(p.Str("cwd"), p.Str("title"), p.Str("body"), base, p.Str("head"))
		},
	}, {
		Name:    "pr_list",
		Summary: "List pull requests",
		Args: []Arg{
			{Name: "state", Type: "string", Desc: "open | closed | merged | all (default open)"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			state := p.Str("state")
			if state == "" {
				state = "open"
			}
			return c.deps.Forge.List(p.Str("cwd"), "", state)
		},
	}, {
		Name:    "pr_view",
		Summary: "Show a pull request, with its checks and review state",
		Args: []Arg{
			{Name: "number", Type: "integer", Desc: "PR number", Required: true},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.deps.Forge.View(p.Str("cwd"), p.Int("number"))
		},
	}, {
		Name:    "pr_merge",
		Summary: "Merge a pull request. Destructive — confirm with the user first",
		Args: []Arg{
			{Name: "number", Type: "integer", Desc: "PR number", Required: true},
			{Name: "squash", Type: "boolean", Desc: "Squash-merge instead of a merge commit"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			if err := c.deps.Forge.Merge(p.Str("cwd"), p.Int("number"), p.Bool("squash")); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "number": p.Int("number")}, nil
		},
	}}
```

- [ ] **Step 5: Wire the real client**

In `controlapi.go`, delete `ghRunner` (lines 44-48) and add:

```go
type forgeClient struct{ app *App }

func (f forgeClient) List(cwd, scope, state string) (any, error) {
	return f.app.ForgePrList(cwd, scope, state)
}
func (f forgeClient) View(cwd string, number int) (any, error) {
	return f.app.ForgePrView(cwd, number)
}
func (f forgeClient) Create(cwd, title, body, base, head string) (any, error) {
	return f.app.ForgePrCreate(cwd, title, body, base, head)
}
func (f forgeClient) Merge(cwd string, number int, squash bool) error {
	return f.app.ForgePrMerge(cwd, number, squash)
}
```

and in the `Deps` literal at line 185, replace `Gh: ghRunner{a},` with
`Forge: forgeClient{a},`.

- [ ] **Step 6: Run the full backend suite**

Run: `cd src-wails && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add src-wails/internal/control/ src-wails/controlapi.go
git commit -m "Route pr_* control verbs through the forge client"
```

---

### Task 8: Frontend data layer — PR composable and sidebar badge

**Files:**
- Modify: `src/composables/usePullRequests.ts` (whole file)
- Modify: `src/stores/git.ts:144-190` (`fetchPr`)
- Test: `src/composables/usePullRequests.test.ts` (create)

**Interfaces:**
- Consumes: wire commands `forge_pr_list`, `forge_pr_view`, `forge_pr_create`, `forge_info` from Task 6.
- Produces: the exported `PullRequest` TS interface and `usePullRequests`'s returned object, both consumed by Task 9.

- [ ] **Step 1: Write the failing test**

Create `src/composables/usePullRequests.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const invoke = vi.fn();
vi.mock("@tauri-apps/api/core", () => ({ invoke: (...a: unknown[]) => invoke(...a) }));

import { usePullRequests } from "./usePullRequests";

describe("usePullRequests", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    invoke.mockReset();
  });

  it("lists through forge_pr_list and never through run_gh", async () => {
    invoke.mockResolvedValue([
      { number: 7, title: "t", url: "u", state: "open", isDraft: false, headRefName: "b", baseRefName: "main" },
    ]);
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(invoke).toHaveBeenCalledWith("forge_pr_list", { cwd: "/repo", scope: "assigned", state: "open" });
    expect(pr.items.value).toHaveLength(1);
    expect(pr.items.value[0].number).toBe(7);
  });

  it("surfaces the backend's own error text", async () => {
    invoke.mockRejectedValue(new Error("glab: not logged in"));
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(pr.error.value).toContain("not logged in");
    expect(pr.items.value).toHaveLength(0);
  });

  it("does not parse JSON — the backend already did", async () => {
    invoke.mockResolvedValue([{ number: 1, title: "t", url: "u", state: "open", isDraft: false, headRefName: "b", baseRefName: "m" }]);
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(pr.items.value[0].title).toBe("t");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test usePullRequests`
Expected: FAIL — the composable still calls `run_gh` and `JSON.parse`s a string.

- [ ] **Step 3: Rewrite the composable**

Replace `src/composables/usePullRequests.ts` with:

```ts
import { computed, ref, shallowRef } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";

export type PrScope = "assigned" | "created" | "all";
export type PrTab = "summary" | "timeline" | "code";

export interface PrCheck { name: string; status?: string; conclusion?: string }
export interface PrFile { path: string; additions: number; deletions: number }
export interface PrComment { author?: string; body: string; createdAt?: string }

// The shape the Go forge package normalizes every provider onto. Optional
// fields are the capability model: a provider that cannot supply checks leaves
// them out and the panel hides that section, with no flag to keep in sync.
export interface PullRequest {
  number: number; title: string; body?: string; url: string;
  state: string; isDraft: boolean;
  headRefName: string; baseRefName: string;
  author?: string; updatedAt?: string;
  additions?: number; deletions?: number;
  checks?: PrCheck[];
  reviewDecision?: string;
  files?: PrFile[];
  comments?: PrComment[];
}

export interface ForgeStatus {
  provider: string; installed: boolean; authed: boolean;
  bin?: string; installCmd?: string; authCmd?: string; docsUrl?: string;
}

function msg(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}

export function usePullRequests(cwd: () => string) {
  const scope = shallowRef<PrScope>("assigned");
  const query = shallowRef("");
  const selected = ref<PullRequest | null>(null);
  const items = ref<PullRequest[]>([]);
  const loading = shallowRef(false);
  const actionLoading = shallowRef(false);
  const error = shallowRef("");
  const activeTab = shallowRef<PrTab>("summary");
  const forge = ref<ForgeStatus | null>(null);

  const visibleItems = computed(() => {
    const needle = query.value.trim().toLowerCase();
    if (!needle) return items.value;
    return items.value.filter((pr) => `${pr.number} ${pr.title} ${pr.headRefName}`.toLowerCase().includes(needle));
  });

  // One call tells the panel which provider this repo is on and whether its CLI
  // is installed and logged in — so the empty state names the right command
  // instead of always saying `gh auth login`.
  async function loadForge() {
    if (!cwd()) return;
    try { forge.value = await invoke<ForgeStatus>("forge_info", { cwd: cwd() }); }
    catch { forge.value = null; }
  }

  async function refresh() {
    if (!cwd()) return;
    loading.value = true; error.value = "";
    try {
      items.value = await invoke<PullRequest[]>("forge_pr_list", {
        cwd: cwd(),
        scope: scope.value === "all" ? "" : scope.value,
        state: "open",
      });
    } catch (e) { error.value = msg(e, "Nelze načíst pull requesty."); items.value = []; }
    finally { loading.value = false; }
  }

  async function select(pr: PullRequest) {
    selected.value = pr; activeTab.value = "summary"; actionLoading.value = true; error.value = "";
    try { selected.value = await invoke<PullRequest>("forge_pr_view", { cwd: cwd(), number: pr.number }); }
    catch (e) { error.value = msg(e, "Nelze načíst detail PR."); }
    finally { actionLoading.value = false; }
  }

  async function merge(squash: boolean) {
    if (!selected.value) return;
    actionLoading.value = true; error.value = "";
    try {
      await invoke("forge_pr_merge", { cwd: cwd(), number: selected.value.number, squash });
      await select(selected.value); await refresh();
    } catch (e) { error.value = msg(e, "Akci se nepodařilo dokončit."); }
    finally { actionLoading.value = false; }
  }

  // The title and body a model writes from the branch's commits and diff, or
  // null so the caller falls back to the last commit message. Best-effort by
  // design: a missing model or a slow answer must not stop the user from
  // opening a PR.
  async function generatedContent(): Promise<{ title: string; body: string } | null> {
    const ui = useUIStore();
    try {
      const head = (await invoke<{ stdout: string }>("run_git", { cwd: cwd(), args: ["branch", "--show-current"] })).stdout.trim();
      const base = (await invoke<{ stdout: string }>("run_git", {
        cwd: cwd(), args: ["symbolic-ref", "--short", "refs/remotes/origin/HEAD"],
      })).stdout.trim().replace(/^origin\//, "");
      if (!head || !base || head === base) return null;
      const out = await invoke<Record<string, string>>("generate_pr_content", {
        cwd: cwd(),
        model: ui.textGenerationModel,
        policy: ui.textGenerationPolicy,
        rules: ui.textGenerationRules,
        baseBranch: base,
        headBranch: head,
      });
      return out.title ? { title: out.title, body: out.body ?? "" } : null;
    } catch {
      return null;
    }
  }

  async function create() {
    actionLoading.value = true; error.value = "";
    try {
      const content = await generatedContent();
      const created = await invoke<PullRequest>("forge_pr_create", {
        cwd: cwd(),
        title: content?.title ?? "",
        body: content?.body ?? "",
        base: "",
        head: "",
      });
      await refresh();
      if (created?.number) await select(created);
    } catch (e) { error.value = msg(e, "Nelze vytvořit PR."); }
    finally { actionLoading.value = false; }
  }

  return {
    scope, query, selected, items: visibleItems, loading, actionLoading, error, activeTab,
    forge, loadForge, refresh, select, merge, create,
  };
}
```

The default-branch read moved off `gh repo view --json defaultBranchRef` onto
`git symbolic-ref refs/remotes/origin/HEAD`, which every provider answers
identically because it is git, not a forge API.

`act(args: string[])` is gone — it took raw gh argv, which cannot survive the
abstraction. Its only caller is the merge button, which Task 9 points at
`merge(squash)`.

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test usePullRequests`
Expected: PASS.

- [ ] **Step 5: Migrate the sidebar badge**

In `src/stores/git.ts`, replace the body of `fetchPr` (the `invoke("run_gh"…)`
block) with:

```ts
    try {
      await ensureBranch(wsId, cwd);
      const pr = await invoke<{
        number: number; state: string; isDraft: boolean;
        checks?: Array<{ conclusion?: string; status?: string }>; url: string;
      }>("forge_pr_view", { cwd, number: 0 });
      prByWs.value[wsId] = pr?.number
        ? { number: pr.number, state: pr.state, isDraft: pr.isDraft, checks: rollupChecks(pr.checks), url: pr.url }
        : null;
    } catch {
      // No PR for this branch, no CLI, not logged in, not a known forge — all
      // the same answer for a badge: don't show one.
      prByWs.value[wsId] = null;
    } finally {
      prInFlight.delete(wsId);
    }
```

`rollupChecks` currently reads gh's `statusCheckRollup` shape. Update it to take
`Array<{ conclusion?: string; status?: string }> | undefined` and count a check
as failing when `conclusion` is `FAILURE`/`failure`/`failed`, passing when it is
`SUCCESS`/`success`, pending otherwise. Read its current body first and keep the
counting semantics the badge already renders.

- [ ] **Step 6: Verify nothing still calls `run_gh`**

Run: `rg -n 'run_gh' src/`
Expected: no output.

Run: `pnpm test && pnpm build`
Expected: PASS — including `commandSurface.test.ts`, which fails on any
`invoke("…")` whose wire name is in neither `remoteAllowed` nor
`CLIENT_SIDE_COMMANDS`.

- [ ] **Step 7: Commit**

```bash
git add src/composables/usePullRequests.ts src/composables/usePullRequests.test.ts src/stores/git.ts
git commit -m "Move PR data path onto the forge commands"
```

---

### Task 9: PR panel — provider-aware empty state and picker

**Files:**
- Modify: `src/components/PullRequestsPanel.vue`
- Modify: `src/components/CommitPushMenu.vue:52-62,84-85,145`

**Interfaces:**
- Consumes: `usePullRequests`'s `forge`, `loadForge`, `merge`, and the `PullRequest` / `ForgeStatus` types from Task 8; `perform` from `src/lib/controlBridge.ts`; `set_forge_provider` from Task 6.
- Produces: nothing other tasks depend on.

- [ ] **Step 1: Read the panel and find every gh assumption**

Run: `rg -n 'gh |statusCheckRollup|reviewDecision|author\.login|act\(' src/components/PullRequestsPanel.vue`

Every hit is a place this task changes: `author.login` becomes `author` (now a
plain string), `statusCheckRollup` becomes `checks`, and `act([...gh argv])`
becomes `merge(squash)`.

- [ ] **Step 2: Replace the empty state**

`PullRequestsPanel.vue:48` currently hardcodes `gh auth login`. Replace that
line with a block driven by `pr.forge`:

```vue
      <div v-else-if="pr.items.value.length === 0" class="flex flex-col items-center gap-2 p-4 text-center leading-relaxed text-muted-foreground">
        <template v-if="!pr.forge.value?.provider">
          <span>Nepoznaný git hosting pro tento repozitář.</span>
          <select
            class="h-8 rounded-[var(--radius-chip)] border border-border bg-hover px-2 text-xs text-foreground"
            @change="setProvider(($event.target as HTMLSelectElement).value)"
          >
            <option value="">Vyber providera…</option>
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="azure">Azure DevOps</option>
            <option value="gitea">Gitea / Forgejo</option>
          </select>
        </template>
        <template v-else-if="!pr.forge.value.installed">
          <span>Chybí <code>{{ pr.forge.value.bin }}</code>. Nainstaluj přes Settings → Integrations.</span>
        </template>
        <template v-else-if="!pr.forge.value.authed">
          <span>Přihlas se přes <code>{{ pr.forge.value.authCmd }}</code>.</span>
        </template>
        <template v-else>
          <span>Žádné pull requesty.</span>
        </template>
      </div>
```

- [ ] **Step 3: Add the picker handler and load forge status**

In the panel's `<script setup>`:

```ts
import { useWorkspaceStore } from "@/stores/workspace";

async function setProvider(provider: string) {
  if (!provider) return;
  const ws = useWorkspaceStore();
  const row = ws.workspaces.find((w) => w.path === props.cwd);
  if (!row) return;
  await invoke("set_forge_provider", { wsId: row.id, provider });
  await pr.loadForge();
  await pr.refresh();
}
```

Call `pr.loadForge()` alongside the existing `pr.refresh()` in whatever
`onMounted` / watcher already drives the panel's initial load.

- [ ] **Step 4: Point the merge button at `merge`**

Find the `act([...])` call sites and replace them. A squash merge becomes
`pr.merge(true)`, a plain merge `pr.merge(false)`. Any other `act` call (close,
reopen, review) has no cross-provider equivalent in this version — delete the
button rather than leaving one that only works on GitHub.

- [ ] **Step 5: Fix the field renames**

`pr.author?.login` → `pr.author`. `pr.statusCheckRollup` → `pr.checks`.
`pr.files` and `pr.comments` keep their names but `comments[].author` is now a
string.

- [ ] **Step 6: Update CommitPushMenu**

`CommitPushMenu.vue:56` has `title="gh pr create --fill"`. Replace with a
provider-neutral `title="Create pull request"`. The `ponytail:` comment at line
84 says the PR gate does not check gh auth — update its wording to say the forge
reports that through `pr.error`, since gh is no longer the only CLI involved.

- [ ] **Step 7: Verify**

Run: `pnpm build && pnpm test`
Expected: PASS — `vue-tsc` is the real gate here; it catches every field rename
the panel missed.

- [ ] **Step 8: Commit**

```bash
git add src/components/PullRequestsPanel.vue src/components/CommitPushMenu.vue
git commit -m "Make the PR panel provider-aware"
```

---

### Task 10: Settings → Integrations CLI installer

**Files:**
- Modify: `src/components/Settings.vue` (Integrations section, after the ntfy block that ends around line 430; script additions near line 1310)

**Interfaces:**
- Consumes: `forge_info` from Task 6, `perform("new_tab", { workspaceId, cmd })` from `src/lib/controlBridge.ts:54`, `useWorkspaceStore`.
- Produces: nothing.

- [ ] **Step 1: Add the provider table to the script**

In `Settings.vue`'s `<script setup>`, below the `// ── Integrations: ntfy.sh ──`
block:

```ts
// ── Integrations: git forge CLIs ──
// The commands live here rather than in Go because they are install advice, not
// behaviour: what Go owns is whether the binary is present (forge_info).
const FORGE_CLIS = [
  { provider: "github", label: "GitHub", bin: "gh", install: "brew install gh", auth: "gh auth login", docs: "https://cli.github.com" },
  { provider: "gitlab", label: "GitLab", bin: "glab", install: "brew install glab", auth: "glab auth login", docs: "https://gitlab.com/gitlab-org/cli" },
  { provider: "azure", label: "Azure DevOps", bin: "az", install: "brew install azure-cli && az extension add --name azure-devops", auth: "az login", docs: "https://learn.microsoft.com/cli/azure/repos/pr" },
  { provider: "gitea", label: "Gitea / Forgejo", bin: "tea", install: "brew install tea", auth: "tea login add", docs: "https://gitea.com/gitea/tea" },
] as const;

const forgeStatus = ref<Record<string, { installed: boolean; authed: boolean }>>({});
const isMac = navigator.platform.toLowerCase().includes("mac");

async function refreshForgeStatus() {
  const ws = useWorkspaceStore();
  const cwd = ws.activeWorkspace?.path ?? "";
  if (!cwd) return;
  // forge_info answers for the repo's own provider; the other rows fall back to
  // "not installed", which is the honest answer when we have not looked.
  const info = await invoke<{ provider: string; installed: boolean; authed: boolean }>("forge_info", { cwd }).catch(() => null);
  if (info?.provider) forgeStatus.value[info.provider] = { installed: info.installed, authed: info.authed };
}

// Runs the command in a terminal tab rather than silently in the background:
// Homebrew can prompt, the output is worth seeing, and the user can kill it.
async function runInTab(cmd: string) {
  const ws = useWorkspaceStore();
  const wsId = ws.activeWorkspace?.id;
  if (!wsId) return;
  await perform("new_tab", { workspaceId: wsId, cmd });
}
```

Import `perform` from `@/lib/controlBridge` and `useWorkspaceStore` if they are
not already imported. Call `refreshForgeStatus()` when the Integrations tab
becomes active.

- [ ] **Step 2: Add the rows to the template**

After the ntfy block inside the Integrations `<section>`:

```vue
          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Git hosting — pull requests</span>
            <div
              v-for="f in FORGE_CLIS"
              :key="f.provider"
              class="flex items-center gap-4 rounded-[var(--radius-card)] border border-border bg-panel px-4 py-3"
            >
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">{{ f.label }}</span>
                <span class="text-[11px] text-muted-foreground">
                  <template v-if="forgeStatus[f.provider]?.authed"><code>{{ f.bin }}</code> — připraveno</template>
                  <template v-else-if="forgeStatus[f.provider]?.installed"><code>{{ f.bin }}</code> nainstalováno, ale nepřihlášeno</template>
                  <template v-else><code>{{ f.bin }}</code> chybí</template>
                </span>
              </div>
              <button
                v-if="isMac"
                class="h-8 rounded-[var(--radius-chip)] border border-border px-3 text-xs text-foreground hover:border-accent"
                @click="runInTab(forgeStatus[f.provider]?.installed ? f.auth : f.install)"
              >
                {{ forgeStatus[f.provider]?.installed ? "Přihlásit" : "Nainstalovat" }}
              </button>
              <a
                v-else
                class="h-8 rounded-[var(--radius-chip)] border border-border px-3 text-xs leading-8 text-foreground hover:border-accent"
                :href="f.docs" target="_blank" rel="noopener"
              >Návod</a>
            </div>
          </div>
```

Off macOS there is no `brew`, so the button becomes a link to the provider's
docs. An apt/winget/choco matrix is not worth writing for a macOS-first app.

- [ ] **Step 3: Verify**

Run: `pnpm build`
Expected: PASS.

Then check by hand: `just dev`, open Settings → Integrations, confirm four rows
render, and that clicking Install opens a terminal tab with the brew command
typed in.

- [ ] **Step 4: Commit**

```bash
git add src/components/Settings.vue
git commit -m "Add git forge CLI rows to Settings integrations"
```

---

### Task 11: Documentation

**Files:**
- Modify: `CLAUDE.md` (the Backend file table, and the control API section)
- Modify: `docs/context.html` (Go bindings list)
- Modify: `docs/burrow.html` (verb registry table)

**Interfaces:**
- Consumes: the finished implementation.
- Produces: nothing.

- [ ] **Step 1: Update CLAUDE.md**

In the backend file list, add after the Git line:

```markdown
- **Pull requests** (`forge.go`, `internal/forge/`) — provider-neutral PR
  operations over each forge's own CLI (`gh`, `glab`, `az repos`, `tea`).
  `internal/forge` owns the `Forge` interface, one normalized `PullRequest`
  struct and the four adapters; it takes an injected `Runner` and never imports
  `main`, so every adapter is tested against captured JSON with no network. The
  provider is detected from the remote URL, with a per-repo override
  (`workspaces.forge_provider`) that a worktree inherits by climbing
  `parent_id`. Optional fields ARE the capability model: a provider that cannot
  supply checks leaves them empty and the panel hides that section, rather than
  the app keeping a capability registry that can drift.
```

Change the Git bullet to say `RunGit` only — `RunGh` no longer exists.

In the control API section, the `pr_*` verbs are described as using gh; change
that to name the forge client.

- [ ] **Step 2: Update docs/context.html**

Find the Go bindings table and replace the `run_gh` row with the six
`forge_*` / `set_forge_provider` commands from Task 6's table.

- [ ] **Step 3: Update docs/burrow.html**

The verb registry table describes `pr_create` as "Open a pull request with the
gh CLI". Change it to match the new summary, and add a line under the verb list
noting that the `pr_*` verbs work on GitHub, GitLab, Azure DevOps and Gitea, and
that which one is used comes from the repo's remote URL.

- [ ] **Step 4: Verify the docs match the code**

Run: `rg -n 'run_gh|RunGh' CLAUDE.md docs/ src/ src-wails/`
Expected: only historical mentions in `docs/changelog.html`, which is a log of
what shipped and must not be rewritten.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/context.html docs/burrow.html
git commit -m "Document the forge package and the forge_* commands"
```
