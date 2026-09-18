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
	// glab prints the new MR's URL, not its JSON. Its number is the only
	// reliable handle on what was just created: re-reading "the MR for the
	// branch in cwd" answers about the checked-out branch, which is wrong
	// whenever the caller passed an explicit Head.
	out, err := g.exec(cwd, args)
	if err != nil {
		return PullRequest{}, err
	}
	if n := numberFromURL(string(out)); n > 0 {
		return g.View(cwd, n)
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
