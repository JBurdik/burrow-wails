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
		HeadRef:   strings.TrimPrefix(p.SourceRefName, "refs/heads/"),
		BaseRef:   strings.TrimPrefix(p.TargetRefName, "refs/heads/"),
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
