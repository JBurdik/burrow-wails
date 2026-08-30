package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// Workspace search behind the ⌘P palette: a literal (non-regex) scan of the
// workspace, ripgrep-first with a `git grep` fallback for machines without rg.
// ponytail: shells out per keystroke-debounce instead of maintaining an index —
// rg over a repo is milliseconds, an index is a whole subsystem to invalidate.

// SearchHit mirrors the frontend's SearchHit interface (src/components/Spotlight.vue).
type SearchHit struct {
	Path string `json:"path"`
	// 1-based match line, or 0 when the hit is a file-name match.
	Line int    `json:"line"`
	Text string `json:"text"`
}

func haveBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// SearchFiles returns file-name matches first, then content matches. Both are
// literal substring matches, case-insensitive unless the query has uppercase
// (ripgrep's --smart-case rule, applied by hand for names).
func (a *App) SearchFiles(cwd, query string, limit int) ([]SearchHit, error) {
	hits := []SearchHit{}
	query = strings.TrimSpace(query)
	if cwd == "" || len(query) < 2 {
		return hits, nil
	}
	if limit <= 0 {
		limit = 30
	}
	nameLimit := limit / 4
	if nameLimit < 4 {
		nameLimit = 4
	}

	rg := haveBin("rg")
	if rg {
		hits = append(hits, searchNames(cwd, query, nameLimit)...)
	}

	var out GitOutput
	if rg {
		out = runCmd("rg", cwd, []string{
			"--line-number", "--no-heading", "--color=never",
			"--smart-case", "--fixed-strings",
			"--max-count", "3", "--max-columns", "220", "--max-filesize", "1M",
			"--", query,
		})
	} else {
		out = runCmd("git", cwd, []string{"grep", "-n", "-I", "--fixed-strings", "-e", query})
	}
	// Exit 1 just means "no matches" for both tools; only a hard failure has stderr.
	for _, line := range strings.Split(out.Stdout, "\n") {
		if len(hits) >= limit {
			break
		}
		if hit, ok := parseGrepLine(line); ok {
			hits = append(hits, hit)
		}
	}
	return hits, nil
}

// searchNames matches the query against paths themselves, so ⌘P doubles as a
// file opener.
func searchNames(cwd, query string, limit int) []SearchHit {
	out := runCmd("rg", cwd, []string{"--files"})
	needle := query
	fold := needle == strings.ToLower(needle)
	if fold {
		needle = strings.ToLower(needle)
	}
	hits := []SearchHit{}
	for _, path := range strings.Split(out.Stdout, "\n") {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		hay := path
		if fold {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, needle) {
			hits = append(hits, SearchHit{Path: path, Line: 0, Text: ""})
			if len(hits) >= limit {
				break
			}
		}
	}
	return hits
}

// parseGrepLine splits `path:line:text`, which both rg and git grep emit. The
// text itself can contain colons, so only the first two are separators.
func parseGrepLine(line string) (SearchHit, bool) {
	if line == "" {
		return SearchHit{}, false
	}
	i := strings.Index(line, ":")
	if i <= 0 {
		return SearchHit{}, false
	}
	rest := line[i+1:]
	j := strings.Index(rest, ":")
	if j <= 0 {
		return SearchHit{}, false
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		return SearchHit{}, false
	}
	text := strings.TrimSpace(rest[j+1:])
	if len(text) > 220 {
		text = text[:220]
	}
	return SearchHit{Path: line[:i], Line: n, Text: text}, true
}
