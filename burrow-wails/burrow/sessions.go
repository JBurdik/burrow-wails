package main

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reads Claude Code's on-disk session JSONL files under
// ~/.claude/projects/<encoded-cwd>/*.jsonl, matching list_claude_sessions/
// read_claude_transcript/read_claude_activity in src-tauri/src/lib.rs.

func claudeProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// encodeProjectDir mirrors Claude Code's own cwd -> directory-name
// encoding (path separators and dots become dashes).
func encodeProjectDir(cwd string) string {
	r := strings.NewReplacer("/", "-", ".", "-")
	return r.Replace(cwd)
}

type ClaudeSessionInfo struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	ModTime   string `json:"modTime"`
}

func (a *App) ListClaudeSessions(cwd string) ([]ClaudeSessionInfo, error) {
	projectsDir, err := claudeProjectsDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(projectsDir, encodeProjectDir(cwd))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []ClaudeSessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, ClaudeSessionInfo{
			SessionID: strings.TrimSuffix(e.Name(), ".jsonl"),
			Path:      filepath.Join(dir, e.Name()),
			ModTime:   info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out, nil
}

// ReadClaudeTranscript returns the raw JSONL lines of a session file.
func (a *App) ReadClaudeTranscript(cwd, sessionID string) ([]string, error) {
	projectsDir, err := claudeProjectsDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(projectsDir, encodeProjectDir(cwd), sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

// ReadClaudeActivity returns just the last line of the transcript — the
// cheap "is anything happening" probe the frontend polls with.
func (a *App) ReadClaudeActivity(cwd, sessionID string) (string, error) {
	lines, err := a.ReadClaudeTranscript(cwd, sessionID)
	if err != nil || len(lines) == 0 {
		return "", err
	}
	return lines[len(lines)-1], nil
}
