package forge

import (
	"encoding/json"
	"fmt"
)

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
	Number            int     `json:"number"`
	Title             string  `json:"title"`
	Body              string  `json:"body"`
	URL               string  `json:"url"`
	State             string  `json:"state"`
	IsDraft           bool    `json:"isDraft"`
	HeadRefName       string  `json:"headRefName"`
	BaseRefName       string  `json:"baseRefName"`
	UpdatedAt         string  `json:"updatedAt"`
	Additions         int     `json:"additions"`
	Deletions         int     `json:"deletions"`
	Author            *ghUser `json:"author"`
	ReviewDecision    string  `json:"reviewDecision"`
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
	out, err := g.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	// gh prints the new PR's URL, not its JSON. Its number is the only reliable
	// handle on what was just created: re-reading "the PR for the branch in cwd"
	// answers about the checked-out branch, which is the wrong one whenever the
	// caller passed an explicit head.
	if n := numberFromURL(string(out)); n > 0 {
		return g.View(cwd, n)
	}
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

// New builds the adapter for a provider. An unknown provider is an error
// rather than a nil Forge, so a caller cannot dereference its way to a panic.
func New(p Provider, run Runner) (Forge, error) {
	switch p {
	case GitHub:
		return &githubForge{run: run}, nil
	}
	return nil, fmt.Errorf("forge: unknown provider %q", p)
}
