package forge

import (
	"strings"
	"testing"
)

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
