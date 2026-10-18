package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Search over every prompt the user sent to Claude Code or Codex, in or out of
// Burrow (Spotlight's PROMPTS section; ported in spirit from Ghostex's
// agent_prompt_search).
//
// Sources are the agents' own prompt logs, read strictly read-only:
//   - Claude: <CLAUDE_CONFIG_DIR|~/.claude>/history.jsonl —
//     {display, pastedContents, project, sessionId, timestamp(ms)}
//   - Codex:  <CODEX_HOME|~/.codex>/history.jsonl — {session_id, ts(s), text};
//     it has no cwd, so that comes from the session's rollout file
//     (sessions/**/rollout-*-<id>.jsonl, first line = session_meta).
//
// Both are append-only interactive-prompt logs: one file each, so the scan is
// a stat and a tail read per file. The per-session transcripts would also hold
// Burrow chats' (headless) prompts — already in chat_fts — and are ~2 GB on a
// working machine; the history logs hold exactly "what I typed".
//
// prompt_files records how many bytes of each log are indexed. A log that
// shrank was rewritten: its rows are dropped and it is read again from 0.

func promptSearchSchema() []string {
	return []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS prompt_fts USING fts5(
			text, agent UNINDEXED, cwd UNINDEXED, session_id UNINDEXED, ts UNINDEXED, src UNINDEXED,
			tokenize = 'unicode61 remove_diacritics 2')`,
		`CREATE TABLE IF NOT EXISTS prompt_files (path TEXT PRIMARY KEY, indexed_bytes INTEGER NOT NULL)`,
	}
}

type promptIndexer struct {
	db        *sql.DB
	claudeDir string
	codexDir  string

	mu       sync.Mutex
	lastScan time.Time
	codexCwd map[string]string // session id → cwd, "" = no rollout found
}

func newPromptIndexer(db *sql.DB) *promptIndexer {
	home, _ := os.UserHomeDir()
	claude := os.Getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	}
	return &promptIndexer{db: db, claudeDir: claude, codexDir: codex}
}

type promptRow struct {
	agent, cwd, sessionID, text string
	ts                          int64 // unix ms
}

// refresh rescans when the last scan is older than maxAge. TryLock: a search
// never waits behind the first (backfill) scan, it searches what is there.
func (p *promptIndexer) refresh(maxAge time.Duration) {
	if p == nil || !p.mu.TryLock() {
		return
	}
	defer p.mu.Unlock()
	if time.Since(p.lastScan) < maxAge {
		return
	}
	p.lastScan = time.Now()
	if err := p.scanFile(filepath.Join(p.claudeDir, "history.jsonl"), parseClaudeHistory); err != nil {
		log.Printf("prompt index (claude): %v", err)
	}
	if err := p.scanFile(filepath.Join(p.codexDir, "history.jsonl"), p.parseCodexHistory); err != nil {
		log.Printf("prompt index (codex): %v", err)
	}
}

// scanFile indexes the complete lines appended to path since the last scan.
func (p *promptIndexer) scanFile(path string, parse func([]byte) (promptRow, bool)) error {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var offset int64
	_ = p.db.QueryRow(`SELECT indexed_bytes FROM prompt_files WHERE path = ?`, path).Scan(&offset)
	if st.Size() == offset {
		return nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if st.Size() < offset {
		if _, err := tx.Exec(`DELETE FROM prompt_fts WHERE src = ?`, path); err != nil {
			return err
		}
		offset = 0
	}
	f, err := os.Open(path) // read-only
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	ins, err := tx.Prepare(`INSERT INTO prompt_fts (text, agent, cwd, session_id, ts, src) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // EOF mid-line: the writer is still on it, take it next time
		}
		offset += int64(len(line))
		row, ok := parse(bytes.TrimSpace(line))
		if !ok {
			continue
		}
		if _, err := ins.Exec(row.text, row.agent, row.cwd, row.sessionID, row.ts, path); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO prompt_files (path, indexed_bytes) VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET indexed_bytes = excluded.indexed_bytes`, path, offset); err != nil {
		return err
	}
	return tx.Commit()
}

// isBareSlashCommand: `/clear`, `/model` are UI, not prompts. `/review the
// parser` keeps its words and stays.
func isBareSlashCommand(s string) bool {
	return strings.HasPrefix(s, "/") && !strings.ContainsAny(s, " \t\n")
}

func parseClaudeHistory(line []byte) (promptRow, bool) {
	var rec struct {
		Display        string `json:"display"`
		Project        string `json:"project"`
		SessionID      string `json:"sessionId"`
		Timestamp      int64  `json:"timestamp"`
		PastedContents map[string]struct {
			Content string `json:"content"`
		} `json:"pastedContents"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return promptRow{}, false
	}
	text := strings.TrimSpace(rec.Display)
	if text == "" || rec.SessionID == "" || isBareSlashCommand(text) {
		return promptRow{}, false
	}
	// Small pastes are inline; big ones are only a hash into paste-cache, and
	// the "[Pasted text #1]" placeholder in display is all there is to index.
	for _, pc := range rec.PastedContents {
		if pc.Content != "" {
			text += "\n" + pc.Content
		}
	}
	return promptRow{agent: "claude", cwd: rec.Project, sessionID: rec.SessionID, text: text, ts: rec.Timestamp}, true
}

func (p *promptIndexer) parseCodexHistory(line []byte) (promptRow, bool) {
	var rec struct {
		SessionID string `json:"session_id"`
		Ts        int64  `json:"ts"`
		Text      string `json:"text"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return promptRow{}, false
	}
	text := strings.TrimSpace(rec.Text)
	if text == "" || rec.SessionID == "" || isBareSlashCommand(text) {
		return promptRow{}, false
	}
	return promptRow{agent: "codex", cwd: p.codexSessionCwd(rec.SessionID), sessionID: rec.SessionID, text: text, ts: rec.Ts * 1000}, true
}

// codexSessionCwd finds the session's rollout by the id in its file name and
// reads the cwd from its first line. The rollout index is built once per scan
// that needs it (a WalkDir over a few hundred names), not per line.
func (p *promptIndexer) codexSessionCwd(sid string) string {
	if cwd, ok := p.codexCwd[sid]; ok {
		return cwd
	}
	if p.codexCwd == nil {
		p.codexCwd = map[string]string{}
	}
	_ = filepath.WalkDir(filepath.Join(p.codexDir, "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") || len(name) < 42 {
			return nil
		}
		id := strings.TrimSuffix(name, ".jsonl")
		id = id[len(id)-36:]
		if _, seen := p.codexCwd[id]; !seen {
			p.codexCwd[id] = rolloutCwd(path)
		}
		return nil
	})
	if _, ok := p.codexCwd[sid]; !ok {
		p.codexCwd[sid] = "" // don't walk again for an id that has no rollout
	}
	return p.codexCwd[sid]
}

// rolloutCwd reads a rollout's session_meta line (`{"type":"session_meta",
// "payload":{"id","cwd",…}}`; very old rollouts had no wrapper).
func rolloutCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadBytes('\n')
	var meta struct {
		Cwd     string `json:"cwd"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(line, &meta)
	return firstNonEmpty(meta.Payload.Cwd, meta.Cwd)
}

// startPromptIndex runs the first (backfill) scan in the background. After
// that SearchPrompts refreshes on use, which is fresher than any interval.
func (a *App) startPromptIndex() {
	if a.db == nil {
		return
	}
	a.prompts = newPromptIndexer(a.db)
	go a.prompts.refresh(0)
}

type PromptSearchHit struct {
	Agent     string `json:"agent"`
	Cwd       string `json:"cwd"`
	SessionID string `json:"session_id"`
	Ts        int64  `json:"ts"`
	Snippet   string `json:"snippet"` // match wrapped in \x02 … \x03
	Text      string `json:"text"`
	// Resumable: the session's transcript still exists. The history logs
	// outlive it (Claude prunes transcripts after cleanupPeriodDays).
	Resumable bool `json:"resumable"`
}

// SearchPrompts returns matching prompts, newest first: every hit already
// holds every word, and in your own history the recent one is the one you
// mean (bm25 ranked year-old prompts over last week's). agent is
// "claude", "codex" or "" for both. Sessions that are Burrow chats are
// excluded — chat_fts already covers them (SearchChats).
func (a *App) SearchPrompts(query, agent string, limit int) ([]PromptSearchHit, error) {
	out := []PromptSearchHit{}
	q := ftsQuery(query)
	if a.db == nil || q == "" {
		return out, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	a.prompts.refresh(10 * time.Second)
	rows, err := a.db.Query(`
		SELECT agent, cwd, session_id, ts, snippet(prompt_fts, 0, char(2), char(3), '…', 14), text
		FROM prompt_fts
		WHERE prompt_fts MATCH ? AND (? = '' OR agent = ?)
		  AND session_id NOT IN (SELECT claude_session_id FROM chats WHERE claude_session_id != '')
		ORDER BY ts DESC
		LIMIT ?`, q, agent, agent, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h PromptSearchHit
		if err := rows.Scan(&h.Agent, &h.Cwd, &h.SessionID, &h.Ts, &h.Snippet, &h.Text); err != nil {
			return nil, err
		}
		h.Resumable = a.prompts.sessionExists(h.Agent, h.SessionID)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (p *promptIndexer) sessionExists(agent, sid string) bool {
	if p == nil || sid == "" || strings.ContainsAny(sid, `*?[\/`) {
		return false
	}
	pattern := filepath.Join(p.claudeDir, "projects", "*", sid+".jsonl")
	if agent == "codex" {
		pattern = filepath.Join(p.codexDir, "sessions", "*", "*", "*", "rollout-*-"+sid+".jsonl")
	}
	m, _ := filepath.Glob(pattern)
	return len(m) > 0
}
