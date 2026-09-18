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
		url                string
		org, project, repo string
		wantErr            bool
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
