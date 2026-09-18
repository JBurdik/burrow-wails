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
	// tea prints the new PR's URL, not its JSON. Its number is the only
	// reliable handle on what was just created: re-reading "the PR for the
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

func (g *giteaForge) Merge(cwd string, number int, squash bool) error {
	args := []string{"pr", "merge", itoa(number)}
	if squash {
		args = append(args, "--style", "squash")
	}
	_, err := g.exec(cwd, args)
	return err
}
