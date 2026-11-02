package main

import (
	"fmt"
	"strconv"
	"strings"

	"burrow/internal/forge"
)

// A chat's linked pull request (ported in principle from t3code's
// linkCreatedPullRequest / refreshPushedPullRequests). chats.pr_number /
// pr_url / pr_state are read-only on the wire like collected_at — SaveChats
// never writes them. The forge is implied by the chat's workspace.
//
// Auto-link happens when a PR is created from a chat's workspace: to the
// calling chat when the `pr_create` verb knows it, else to every live chat on
// that workspace whose branch matches. Unlinking writes a tombstone
// (chat_pr_dismissed) so an auto-link never re-adds a PR the user removed; an
// explicit manual link clears it. No background polling: the state is refreshed
// after a push from Burrow and when the thread details card opens.

// prGlyphState folds draft into the state the Sidebar glyph renders.
func prGlyphState(pr forge.PullRequest) string {
	if pr.State == "open" && pr.IsDraft {
		return "draft"
	}
	return pr.State
}

func (a *App) setChatPr(chatID int64, pr forge.PullRequest) error {
	_, err := a.db.Exec(`UPDATE chats SET pr_number = ?, pr_url = ?, pr_state = ? WHERE id = ?`,
		pr.Number, pr.URL, prGlyphState(pr), chatID)
	return err
}

// linkChatPr links one chat. A manual link clears the tombstone; an automatic
// one is skipped by it. Reports whether the row changed.
func (a *App) linkChatPr(chatID int64, pr forge.PullRequest, manual bool) (bool, error) {
	if a.db == nil {
		return false, fmt.Errorf("no database")
	}
	if pr.Number <= 0 {
		return false, fmt.Errorf("pull request has no number")
	}
	if manual {
		if _, err := a.db.Exec(`DELETE FROM chat_pr_dismissed WHERE chat_id = ? AND pr_number = ?`, chatID, pr.Number); err != nil {
			return false, err
		}
	} else {
		var n int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_pr_dismissed WHERE chat_id = ? AND pr_number = ?`, chatID, pr.Number).Scan(&n)
		if n > 0 {
			return false, nil
		}
	}
	if err := a.setChatPr(chatID, pr); err != nil {
		return false, err
	}
	return true, nil
}

// autoLinkPr links a PR just created in cwd. callerChat > 0 links only that
// chat; otherwise every unarchived chat on the workspace at cwd whose branch is
// the PR's head (or unrecorded). Never fails the create — a link that cannot be
// made is dropped.
func (a *App) autoLinkPr(cwd string, callerChat int64, pr forge.PullRequest) {
	if a.db == nil || pr.Number <= 0 {
		return
	}
	var ids []int64
	if callerChat > 0 {
		ids = []int64{callerChat}
	} else {
		rows, err := a.db.Query(`SELECT c.id FROM chats c JOIN workspaces w ON w.id = c.workspace_id
			WHERE w.path = ? AND c.archived_at = 0 AND (c.branch = '' OR c.branch = ?)`, cwd, pr.HeadRef)
		if err != nil {
			return
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	changed := false
	for _, id := range ids {
		if ok, _ := a.linkChatPr(id, pr, false); ok {
			changed = true
		}
	}
	if changed {
		busEmit("chats-changed", nil)
	}
}

// unlinkChatPr clears the link and tombstones the PR it pointed at.
func (a *App) unlinkChatPr(chatID int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	var n int64
	if err := a.db.QueryRow(`SELECT pr_number FROM chats WHERE id = ?`, chatID).Scan(&n); err != nil {
		return fmt.Errorf("chat %d: %w", chatID, err)
	}
	if n > 0 {
		if _, err := a.db.Exec(`INSERT OR IGNORE INTO chat_pr_dismissed (chat_id, pr_number) VALUES (?, ?)`, chatID, n); err != nil {
			return err
		}
	}
	_, err := a.db.Exec(`UPDATE chats SET pr_number = 0, pr_url = '', pr_state = '' WHERE id = ?`, chatID)
	return err
}

// parsePrRef reads "#123", "123" or a PR/MR URL into a number. The client
// parses the same shapes (src/lib/prLinkInput.ts) to decide what it accepts;
// this is the server's own check, not a trust of the client's.
func parsePrRef(ref string) (n int, isURL bool) {
	ref = strings.TrimSpace(ref)
	if strings.Contains(ref, "://") {
		f := strings.FieldsFunc(strings.SplitN(strings.SplitN(ref, "?", 2)[0], "#", 2)[0], func(r rune) bool { return r == '/' })
		// .../pull/42, .../merge_requests/7, .../pulls/3, .../pullrequest/9 —
		// optionally followed by /files, /commits, /diffs.
		for i := len(f) - 2; i >= 0; i-- {
			switch f[i] {
			case "pull", "pulls", "merge_requests", "pullrequest":
				if v, err := strconv.Atoi(f[i+1]); err == nil && v > 0 {
					return v, true
				}
			}
		}
		return 0, true
	}
	if v, err := strconv.Atoi(strings.TrimPrefix(ref, "#")); err == nil && v > 0 {
		return v, false
	}
	return 0, false
}

// ChatLinkPr links a PR to a chat by "#N", "N" or URL, looked up on the chat's
// workspace forge. A URL from another repository is refused rather than
// silently linking that number here.
func (a *App) ChatLinkPr(chatID int64, ref string) (forge.PullRequest, error) {
	n, isURL := parsePrRef(ref)
	if n <= 0 {
		return forge.PullRequest{}, fmt.Errorf("not a pull request number or URL: %q", ref)
	}
	c, err := a.chatByID(chatID)
	if err != nil {
		return forge.PullRequest{}, err
	}
	pr, err := a.ForgePrView(a.chatCwd(c), n)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if isURL && !sameURL(pr.URL, ref) {
		return forge.PullRequest{}, fmt.Errorf("%s is not a pull request of this repository (#%d here is %s)", ref, n, pr.URL)
	}
	if _, err := a.linkChatPr(chatID, pr, true); err != nil {
		return forge.PullRequest{}, err
	}
	busEmit("chats-changed", nil)
	return pr, nil
}

// sameURL compares a pasted URL with the forge's, ignoring scheme case,
// a trailing slash and any /files-style suffix after the number.
func sameURL(canonical, pasted string) bool {
	norm := func(s string) string {
		return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "/")
	}
	c, p := norm(canonical), norm(strings.SplitN(strings.SplitN(pasted, "?", 2)[0], "#", 2)[0])
	return p == c || strings.HasPrefix(p, c+"/")
}

func (a *App) ChatUnlinkPr(chatID int64) error {
	if err := a.unlinkChatPr(chatID); err != nil {
		return err
	}
	busEmit("chats-changed", nil)
	return nil
}

// ChatPrRefresh re-reads a chat's linked PR (state, checks) for the details
// card, and records the new state for the Sidebar glyph.
func (a *App) ChatPrRefresh(chatID int64) (forge.PullRequest, error) {
	c, err := a.chatByID(chatID)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if c.PrNumber <= 0 {
		return forge.PullRequest{}, fmt.Errorf("chat %d has no linked pull request", chatID)
	}
	pr, err := a.ForgePrView(a.chatCwd(c), int(c.PrNumber))
	if err != nil {
		return forge.PullRequest{}, err
	}
	if prGlyphState(pr) != c.PrState || pr.URL != c.PrURL {
		if err := a.setChatPr(chatID, pr); err == nil {
			busEmit("chats-changed", nil)
		}
	}
	return pr, nil
}

// ChatPrsRefresh refreshes every linked PR of the chats on the
// workspace at cwd — called after a push from Burrow. One forge call per
// distinct PR number.
func (a *App) ChatPrsRefresh(cwd string) error {
	if a.db == nil {
		return nil
	}
	rows, err := a.db.Query(`SELECT c.id, c.pr_number, c.pr_url, c.pr_state FROM chats c JOIN workspaces w ON w.id = c.workspace_id
		WHERE w.path = ? AND c.pr_number > 0`, cwd)
	if err != nil {
		return err
	}
	type linked struct {
		id, n      int64
		url, state string
	}
	var all []linked
	for rows.Next() {
		var l linked
		if rows.Scan(&l.id, &l.n, &l.url, &l.state) == nil {
			all = append(all, l)
		}
	}
	rows.Close()
	seen := map[int64]forge.PullRequest{}
	changed := false
	for _, l := range all {
		pr, ok := seen[l.n]
		if !ok {
			if pr, err = a.ForgePrView(cwd, int(l.n)); err != nil {
				continue
			}
			seen[l.n] = pr
		}
		if prGlyphState(pr) != l.state || pr.URL != l.url {
			if a.setChatPr(l.id, pr) == nil {
				changed = true
			}
		}
	}
	if changed {
		busEmit("chats-changed", nil)
	}
	return nil
}
