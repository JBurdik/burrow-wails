package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func newPromptApp(t *testing.T) (*App, string, string) {
	a, dir := newChatApp(t)
	claude, codex := filepath.Join(dir, "claude"), filepath.Join(dir, "codex")
	a.prompts = &promptIndexer{db: a.db, claudeDir: claude, codexDir: codex}
	return a, filepath.Join(claude, "history.jsonl"), filepath.Join(codex, "history.jsonl")
}

func promptSearch(t *testing.T, a *App, q, agent string) []PromptSearchHit {
	t.Helper()
	a.prompts.lastScan = a.prompts.lastScan.AddDate(-1, 0, 0) // force a rescan
	hits, err := a.SearchPrompts(q, agent, 20)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return hits
}

func TestSearchPromptsBothAgentsIncremental(t *testing.T) {
	a, claudeHist, codexHist := newPromptApp(t)
	codexDir := filepath.Dir(codexHist)
	sid := "01a0a45f-b26a-7d81-bdd9-5d872fbd6f98"
	appendFile(t, filepath.Join(codexDir, "sessions/2026/09/15/rollout-2026-09-15T11-22-10-"+sid+".jsonl"),
		`{"timestamp":"x","type":"session_meta","payload":{"id":"`+sid+`","cwd":"/work/api"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"ignored"}}`+"\n")

	appendFile(t, claudeHist,
		`{"display":"Přepiš parser konfigurace","pastedContents":{"1":{"id":1,"type":"text","content":"zebraword inline paste"}},"project":"/work/web","sessionId":"c-1","timestamp":1792319507090}`+"\n"+
			`{"display":"/clear","pastedContents":{},"project":"/work/web","sessionId":"c-1","timestamp":1792319507091}`+"\n"+
			`not json`+"\n")
	appendFile(t, codexHist, `{"session_id":"`+sid+`","ts":1792135782,"text":"fix the parser tests"}`+"\n")

	hits := promptSearch(t, a, "parser", "")
	if len(hits) != 2 {
		t.Fatalf("want a hit per agent, got %+v", hits)
	}
	byAgent := map[string]PromptSearchHit{}
	for _, h := range hits {
		byAgent[h.Agent] = h
	}
	c, x := byAgent["claude"], byAgent["codex"]
	if c.Cwd != "/work/web" || c.SessionID != "c-1" || c.Ts != 1792319507090 {
		t.Errorf("claude hit = %+v", c)
	}
	if c.Resumable {
		t.Errorf("claude session has no transcript, must not be resumable")
	}
	if x.Cwd != "/work/api" || x.SessionID != sid || x.Ts != 1792135782000 || x.Text != "fix the parser tests" || !x.Resumable {
		t.Errorf("codex hit = %+v (cwd comes from the rollout)", x)
	}
	if !strings.Contains(x.Snippet, "\x02parser\x03") {
		t.Errorf("snippet must mark the match: %q", x.Snippet)
	}
	if got := promptSearch(t, a, "prepis kon", ""); len(got) != 1 {
		t.Errorf("diacritics-insensitive prefix search: %+v", got)
	}
	if got := promptSearch(t, a, "zebraword", ""); len(got) != 1 {
		t.Errorf("inline paste content must be indexed: %+v", got)
	}
	if got := promptSearch(t, a, "clear", ""); len(got) != 0 {
		t.Errorf("bare slash command must not be indexed: %+v", got)
	}
	if got := promptSearch(t, a, "parser", "codex"); len(got) != 1 || got[0].Agent != "codex" {
		t.Errorf("agent filter: %+v", got)
	}

	// Append + a half-written line: only the complete line is taken, nothing twice.
	appendFile(t, claudeHist, `{"display":"parser again","project":"/w","sessionId":"c-2","timestamp":1}`+"\n"+`{"display":"parser half`)
	if got := promptSearch(t, a, "parser", "claude"); len(got) != 2 {
		t.Fatalf("incremental scan: %+v", got)
	}
	appendFile(t, claudeHist, ` done","project":"/w","sessionId":"c-3","timestamp":2}`+"\n")
	if got := promptSearch(t, a, "parser", "claude"); len(got) != 3 {
		t.Fatalf("finished line must be picked up once: %+v", got)
	}

	// A rewritten (shorter) log is re-read from scratch, old rows dropped.
	if err := os.WriteFile(claudeHist, []byte(`{"display":"parser fresh","project":"/w","sessionId":"c-9","timestamp":3}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := promptSearch(t, a, "parser", "claude"); len(got) != 1 || got[0].SessionID != "c-9" {
		t.Fatalf("truncated log: %+v", got)
	}

	// A Burrow chat's session is covered by chat_fts, not repeated here.
	if _, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "t", ClaudeSessionID: sid}); err != nil {
		t.Fatal(err)
	}
	if got := promptSearch(t, a, "parser", "codex"); len(got) != 0 {
		t.Errorf("chat session must be excluded: %+v", got)
	}
}
