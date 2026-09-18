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

// seqRunner replays a different answer per call, for adapters that make more
// than one call in a single method.
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
		return "", "cli said no", code
	}
	return out, "", 0
}

func TestGithubCreateLooksUpTheCreatedPR(t *testing.T) {
	// First call is `pr create` and prints a URL; second is the `pr view` that
	// follows. The assertion is that the view carries the created PR's number,
	// not a bare "current branch" view.
	s := &seqRunner{outs: []string{"https://github.com/a/b/pull/99\n", ghViewJSON}}
	f, _ := New(GitHub, s.run)
	if _, err := f.Create("/repo", CreateOpts{Title: "t", Body: "b", Head: "other-branch"}); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 {
		t.Fatalf("expected create then view, got %v", s.calls)
	}
	argv := strings.Join(s.calls[1], " ")
	if !strings.Contains(argv, "pr view 99") {
		t.Errorf("view did not target the created PR: %s", argv)
	}
}

func TestNumberFromURL(t *testing.T) {
	cases := map[string]int{
		"https://github.com/a/b/pull/42":            42,
		"https://gitlab.com/a/b/-/merge_requests/7": 7,
		"https://codeberg.org/a/b/pulls/3\n":        3,
		"no number here":                            0,
		"":                                          0,
	}
	for in, want := range cases {
		if got := numberFromURL(in); got != want {
			t.Errorf("numberFromURL(%q) = %d, want %d", in, got, want)
		}
	}
}
