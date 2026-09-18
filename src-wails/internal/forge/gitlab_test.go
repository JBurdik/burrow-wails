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
	// glab has no --opened flag; the default/open case must rely on glab's own
	// default rather than passing one. A future re-add of a state flag here
	// has to be a conscious change to this test.
	if strings.Contains(argv, "--opened") {
		t.Errorf("must not pass --opened, glab has no such flag: %s", argv)
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

// Create must look up the MR it just created, not "the MR for the branch
// checked out in cwd" — the caller may have passed an explicit Head.
func TestGitlabCreateLooksUpTheCreatedMR(t *testing.T) {
	s := &seqRunner{outs: []string{"https://gitlab.com/a/b/-/merge_requests/7\n", glViewJSON}}
	f, _ := New(GitLab, s.run)
	if _, err := f.Create("/repo", CreateOpts{Title: "t", Body: "b", Head: "other-branch"}); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 {
		t.Fatalf("expected create then view, got %v", s.calls)
	}
	argv := strings.Join(s.calls[1], " ")
	if !strings.Contains(argv, "mr view 7") {
		t.Errorf("view did not target the created MR: %s", argv)
	}
}
