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
	"strings"
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

// numberFromURL pulls the trailing pull/merge request number out of the URL a
// create command prints — .../pull/42, .../merge_requests/7, .../pulls/3 all
// end the same way. Returns 0 when there is no number to find, which callers
// treat as "ask the CLI instead".
func numberFromURL(s string) int {
	f := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool { return r == '/' })
	for i := len(f) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(f[i]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

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
