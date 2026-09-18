package forge

import (
	"fmt"
	"strings"
)

// Detect maps a git remote URL onto a provider, or "" when the host says
// nothing. A self-hosted GitLab at git.firma.cz is indistinguishable from
// anything else by hostname, which is why the caller keeps a per-repo override
// and a picker for the "" case.
func Detect(remoteURL string) Provider {
	host := remoteHost(remoteURL)
	switch {
	case host == "":
		return ""
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return GitHub
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return GitLab
	case host == "dev.azure.com" || host == "ssh.dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com"):
		return Azure
	case host == "codeberg.org":
		return Gitea
	}
	return ""
}

// remoteHost pulls the host out of both URL forms git uses: an scp-style
// "git@host:path" and a real URL "scheme://[user@]host/path".
func remoteHost(remoteURL string) string {
	s := strings.TrimSpace(remoteURL)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else if at := strings.Index(s, "@"); at >= 0 && strings.Index(s, ":") > at {
		// scp-style: git@host:path
		s = s[at+1:]
		if c := strings.Index(s, ":"); c >= 0 {
			return strings.ToLower(s[:c])
		}
		return strings.ToLower(s)
	}
	if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	if i := strings.IndexAny(s, "/:"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// ParseAzureRemote extracts the organization, project and repository the
// az CLI needs as explicit flags. Azure is the one provider whose CLI is not
// repo-aware from cwd alone, so this has to come out of the remote URL — the
// alternative is asking the user for three values they already committed.
//
// Three shapes exist in the wild:
//
//	https://dev.azure.com/{org}/{project}/_git/{repo}
//	git@ssh.dev.azure.com:v3/{org}/{project}/{repo}
//	https://{org}.visualstudio.com/{project}/_git/{repo}
func ParseAzureRemote(remoteURL string) (org, project, repo string, err error) {
	host := remoteHost(remoteURL)
	s := strings.TrimSuffix(strings.TrimSpace(remoteURL), ".git")

	if strings.HasSuffix(host, ".visualstudio.com") {
		org = strings.TrimSuffix(host, ".visualstudio.com")
		parts := pathSegments(s, host)
		// {project}/_git/{repo}
		if len(parts) >= 3 && parts[len(parts)-2] == "_git" {
			return org, parts[len(parts)-3], parts[len(parts)-1], nil
		}
		return "", "", "", fmt.Errorf("forge: cannot read project/repo out of Azure remote %q", remoteURL)
	}

	parts := pathSegments(s, host)
	// scp-style carries a leading "v3" segment.
	if len(parts) > 0 && parts[0] == "v3" {
		parts = parts[1:]
	}
	// Drop the "_git" marker so both shapes collapse to org/project/repo.
	filtered := parts[:0]
	for _, p := range parts {
		if p != "_git" && p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) < 3 {
		return "", "", "", fmt.Errorf("forge: cannot read org/project/repo out of Azure remote %q", remoteURL)
	}
	return filtered[0], filtered[1], filtered[2], nil
}

func pathSegments(remoteURL, host string) []string {
	s := remoteURL
	if i := strings.Index(s, host); i >= 0 {
		s = s[i+len(host):]
	}
	s = strings.TrimLeft(s, ":/")
	return strings.Split(s, "/")
}

// normState collapses each provider's vocabulary onto open|merged|closed.
// Azure says "completed"/"abandoned", GitLab says "opened", GitHub SHOUTS.
func normState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "open", "opened", "active":
		return "open"
	case "merged", "completed":
		return "merged"
	case "closed", "abandoned", "declined":
		return "closed"
	}
	return ""
}
