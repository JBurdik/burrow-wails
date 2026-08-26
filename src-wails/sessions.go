package main

import (
	"bufio"
	"encoding/json"
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

// ClaudeSessionInfo mirrors ClaudeChat.vue's local ClaudeSessionInfo
// interface (session_id, first_message, updated_at — an ISO string).
type ClaudeSessionInfo struct {
	SessionID    string `json:"session_id"`
	FirstMessage string `json:"first_message"`
	UpdatedAt    string `json:"updated_at"`
}

// jsonlFirstUserText scans a session file for the first user-turn text, for
// the session picker's preview line. Best-effort: returns "" on any shape
// mismatch rather than failing the whole list.
func jsonlFirstUserText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "user" && entry.Message.Role != "user" {
			continue
		}
		switch c := entry.Message.Content.(type) {
		case string:
			return c
		case []any:
			for _, block := range c {
				if m, ok := block.(map[string]any); ok {
					if text, ok := m["text"].(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
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
		path := filepath.Join(dir, e.Name())
		out = append(out, ClaudeSessionInfo{
			SessionID:    strings.TrimSuffix(e.Name(), ".jsonl"),
			FirstMessage: jsonlFirstUserText(path),
			UpdatedAt:    info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
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
