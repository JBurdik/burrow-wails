package main

import (
	"testing"

	"burrow/internal/forge"
)

func prTestChats(t *testing.T) (*App, int64, int64, int64) {
	t.Helper()
	a := newTestApp(t)
	res, err := a.db.Exec(`INSERT INTO workspaces (name, path, created_at) VALUES ('w', '/repo', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := res.LastInsertId()
	mk := func(branch string, archived int64) int64 {
		c, err := a.CreateChat(Chat{WorkspaceID: ws, Branch: branch, ArchivedAt: archived})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	return a, mk("feat/x", 0), mk("other", 0), mk("feat/x", 1)
}

func prOf(t *testing.T, a *App, id int64) Chat {
	t.Helper()
	c, err := a.chatByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var testPR = forge.PullRequest{Number: 42, URL: "https://github.com/a/b/pull/42", State: "open", IsDraft: true, HeadRef: "feat/x"}

func TestAutoLinkMatchesWorkspaceAndBranch(t *testing.T) {
	a, onBranch, other, archived := prTestChats(t)
	a.autoLinkPr("/repo", 0, testPR)
	if c := prOf(t, a, onBranch); c.PrNumber != 42 || c.PrURL != testPR.URL || c.PrState != "draft" {
		t.Errorf("chat on the PR branch not linked: %+v", c)
	}
	if c := prOf(t, a, other); c.PrNumber != 0 {
		t.Errorf("chat on another branch linked: %d", c.PrNumber)
	}
	if c := prOf(t, a, archived); c.PrNumber != 0 {
		t.Errorf("archived chat linked: %d", c.PrNumber)
	}
}

func TestAutoLinkPrefersCallerChat(t *testing.T) {
	a, onBranch, other, _ := prTestChats(t)
	a.autoLinkPr("/repo", other, testPR)
	if prOf(t, a, other).PrNumber != 42 || prOf(t, a, onBranch).PrNumber != 0 {
		t.Error("caller chat should be the only one linked")
	}
}

func TestUnlinkTombstoneBlocksAutoLinkNotManual(t *testing.T) {
	a, id, _, _ := prTestChats(t)
	a.autoLinkPr("/repo", 0, testPR)
	if err := a.unlinkChatPr(id); err != nil {
		t.Fatal(err)
	}
	if c := prOf(t, a, id); c.PrNumber != 0 || c.PrURL != "" || c.PrState != "" {
		t.Fatalf("unlink left %+v", c)
	}
	a.autoLinkPr("/repo", 0, testPR)
	a.autoLinkPr("/repo", id, testPR)
	if prOf(t, a, id).PrNumber != 0 {
		t.Fatal("auto-link re-added a dismissed PR")
	}
	// A different PR is not covered by the tombstone.
	a.autoLinkPr("/repo", id, forge.PullRequest{Number: 43, State: "open"})
	if prOf(t, a, id).PrNumber != 43 {
		t.Fatal("tombstone blocked an unrelated PR")
	}
	// A manual link of the dismissed PR clears its tombstone.
	if _, err := a.linkChatPr(id, testPR, true); err != nil {
		t.Fatal(err)
	}
	if prOf(t, a, id).PrNumber != 42 {
		t.Fatal("manual link refused")
	}
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_pr_dismissed WHERE chat_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("manual link left %d tombstones", n)
	}
}

// SaveChats is a client's whole-row save: it must not clear a link it never heard of.
func TestSaveChatsLeavesPrLink(t *testing.T) {
	a, id, _, _ := prTestChats(t)
	a.autoLinkPr("/repo", id, testPR)
	c := prOf(t, a, id)
	c.PrNumber, c.PrURL, c.PrState = 0, "", ""
	if err := a.SaveChats([]Chat{c}); err != nil {
		t.Fatal(err)
	}
	if prOf(t, a, id).PrNumber != 42 {
		t.Error("SaveChats overwrote pr_number")
	}
}

func TestParsePrRef(t *testing.T) {
	cases := []struct {
		in    string
		n     int
		isURL bool
	}{
		{"#12", 12, false},
		{" 7 ", 7, false},
		{"https://github.com/a/b/pull/42", 42, true},
		{"https://github.com/a/b/pull/42/files?x=1", 42, true},
		{"https://gitlab.com/g/p/-/merge_requests/7", 7, true},
		{"https://codeberg.org/a/b/pulls/3", 3, true},
		{"https://dev.azure.com/o/p/_git/r/pullrequest/9", 9, true},
		{"https://github.com/a/b/issues/4", 0, true},
		{"nope", 0, false},
		{"#0", 0, false},
	}
	for _, c := range cases {
		n, isURL := parsePrRef(c.in)
		if n != c.n || isURL != c.isURL {
			t.Errorf("parsePrRef(%q) = %d,%v want %d,%v", c.in, n, isURL, c.n, c.isURL)
		}
	}
	if !sameURL("https://github.com/a/b/pull/42", "HTTPS://github.com/a/b/pull/42/files") || sameURL("https://github.com/a/b/pull/42", "https://github.com/x/y/pull/42") {
		t.Error("sameURL")
	}
}
