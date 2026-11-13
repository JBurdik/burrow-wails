package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// "Threads already on it" (threadbear's sameWork): a new thread's first
// message is compared with the open threads — plain word overlap, no model,
// nothing leaves the machine — and when some look like the same work, the
// agent is told about them (hidden context on that first message) and a note
// row says which. The agent decides; nothing is merged or stopped.

type sameWorkHit struct {
	ID    int64
	Title string
	Recap string
	Score float64
}

// Words that say nothing about WHAT the work is (en + cs).
var sameWorkStop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`this that with from have will would could should please make into about there their what when where which while
		them then than also just only some more like want need help code file files thread chat burrow
		tento tato toto jsem jsou bude budou prosím udělej udělat můžeš chci potřebuju potřebuji nějak něco kterou který která které
		jako aby když nebo také taky ještě tady teď hned soubor soubory`) {
		sameWorkStop[w] = true
	}
}

func sameWorkTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 4 && !sameWorkStop[w] {
			out[w] = true
		}
	}
	return out
}

// sameWorkScore: shared words over the smaller set; 0 below two shared words
// (one common word is noise).
func sameWorkScore(a, b map[string]bool) float64 {
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	if shared < 2 {
		return 0
	}
	small := len(a)
	if len(b) < small {
		small = len(b)
	}
	return float64(shared) / float64(small)
}

const sameWorkThreshold = 0.34

// findSameWork ranks the open top-level threads against `text`; a thread in
// the same repo gets a small boost. At most three.
func (a *App) findSameWork(chatID int64, text string) []sameWorkHit {
	if a.db == nil {
		return nil
	}
	mine := sameWorkTokens(text)
	if len(mine) < 2 {
		return nil
	}
	repoOf := func(wsID int64) int64 {
		var parent int64
		_ = a.db.QueryRow(`SELECT COALESCE(parent_id, 0) FROM workspaces WHERE id = ?`, wsID).Scan(&parent)
		if parent > 0 {
			return parent
		}
		return wsID
	}
	self, err := a.chatByID(chatID)
	if err != nil {
		return nil
	}
	myRepo := repoOf(self.WorkspaceID)
	rows, err := a.db.Query(`SELECT c.id, c.workspace_id, c.title, c.recap,
			COALESCE((SELECT s.line FROM chat_stream s WHERE s.chat_id = c.id AND s.kind = ? ORDER BY s.ord LIMIT 1), '')
		FROM chats c WHERE c.id != ? AND c.archived_at = 0 AND c.parent_chat_id = 0 AND c.kind != 'side'`, chatUserKind, chatID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var hits []sameWorkHit
	for rows.Next() {
		var h sameWorkHit
		var ws int64
		var first string
		if rows.Scan(&h.ID, &ws, &h.Title, &h.Recap, &first) != nil {
			continue
		}
		h.Score = sameWorkScore(mine, sameWorkTokens(h.Title+" "+h.Recap+" "+first))
		if h.Score > 0 && repoOf(ws) == myRepo {
			h.Score += 0.15
		}
		if h.Score >= sameWorkThreshold {
			hits = append(hits, h)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return hits
}

// sameWorkContext is the hidden note the agent gets with its first message.
func sameWorkContext(hits []sameWorkHit) string {
	var b strings.Builder
	b.WriteString("[Burrow] Open threads that may already be doing this work — read any that look the same (burrow chat_read <id>) before starting. If one already covers it, say so and pass anything new to it (burrow chat_send) instead of doing the work twice, or ask the user which thread should own it:\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "- chat %d: %q", h.ID, h.Title)
		if h.Recap != "" {
			fmt.Fprintf(&b, " — %s", h.Recap)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sameWorkNote is the row the user sees.
func sameWorkNote(hits []sameWorkHit) string {
	titles := make([]string, len(hits))
	for i, h := range hits {
		titles[i] = fmt.Sprintf("“%s”", h.Title)
	}
	return "Possibly the same work as: " + strings.Join(titles, ", ") + " — the agent was told to check first."
}

// firstUserSend: no prompt in this chat's stream yet.
func (a *App) firstUserSend(chatID int64) bool {
	var one int
	return a.db.QueryRow(`SELECT 1 FROM chat_stream WHERE chat_id = ? AND kind = ? LIMIT 1`, chatID, chatUserKind).Scan(&one) != nil
}
