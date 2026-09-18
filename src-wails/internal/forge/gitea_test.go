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

// tea prints the new PR's URL, not its JSON. Create must use that number
// rather than re-reading "the PR for the branch in cwd", which is wrong
// whenever the caller passed an explicit Head.
func TestGiteaCreateLooksUpTheCreatedPR(t *testing.T) {
	viewJSON := `{"number":9,"state":"open","html_url":"https://codeberg.org/a/b/pulls/9",
	 "head":{"ref":"other-branch"},"base":{"ref":"main"}}`
	s := &seqRunner{outs: []string{"https://codeberg.org/a/b/pulls/9\n", viewJSON}}
	f, _ := New(Gitea, s.run)
	if _, err := f.Create("/repo", CreateOpts{Title: "t", Body: "b", Head: "other-branch"}); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 {
		t.Fatalf("expected create then view, got %v", s.calls)
	}
	argv := strings.Join(s.calls[1], " ")
	if !strings.Contains(argv, "pr 9") {
		t.Errorf("view did not target the created PR: %s", argv)
	}
}
