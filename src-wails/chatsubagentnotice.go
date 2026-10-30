package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"burrow/internal/agentphase"
)

// Sub-agent notices: a thread no longer has to poll collect_results /
// wait_result. When one of its sub-agents finishes a turn ("done") or blocks
// on a question / permission ("blocked"), Go records a notice and delivers it
// into the PARENT chat as a prompt (t3code's DelegatedCompletionDelivery +
// DelegatedTaskQuestionReactor, onto Burrow's chat_schedules pattern).
//
// Recorded where the phase changes (emitChatLine), delivered by the schedule
// sweep (chatScheduleLoop, ahead of scheduled prompts) and only once the parent
// has settled: ChatSend into a running turn is a steer, not a queued prompt.
// The key is per (child, turn) — the phase's TurnEndedAt for a completion, its
// UpdatedAt on entering waiting_* for a blocked episode — and the row is the
// idempotency record: INSERT OR IGNORE on enqueue, delivered_at claimed before
// the send, so a retried sweep never sends one twice.
//
// Ordering vs. the user's queued follow-ups (chatqueue.go): a notice goes
// first — deliveryOrder in chatdelivery.go.
//
// Off with uiPrefs.subagentAutoDeliver = false (Settings → General), read
// fresh at enqueue; side chats and detached chats (no parent) never notify.

const (
	subagentNoticeMaxChars    = 4000
	subagentNoticeMaxAttempts = 5
	// Delivered rows are kept a day as the idempotency record, then pruned.
	subagentNoticeKeep = 24 * time.Hour
)

func subagentNoticesSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS subagent_notices (
			child_id     INTEGER NOT NULL,
			key          TEXT    NOT NULL,
			parent_id    INTEGER NOT NULL,
			kind         TEXT    NOT NULL,
			summary      TEXT    NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL,
			delivered_at INTEGER NOT NULL DEFAULT 0,
			attempts     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (child_id, key)
		)`,
	}
}

// subagentAutoDeliver reads uiPrefs.subagentAutoDeliver fresh; absent = on,
// unreadable config = off (never prompt a chat on a guess).
func (a *App) subagentAutoDeliver() bool {
	raw, err := a.ReadConfig()
	if err != nil {
		return false
	}
	var cfg struct {
		UI struct {
			On *bool `json:"subagentAutoDeliver"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return false
	}
	return cfg.UI.On == nil || *cfg.UI.On
}

// noteSubagentPhase records a notice when a sub-agent's phase crosses into a
// finished turn or a blocked one. ev is the event that moved it (the request
// for a blocked one, summarized for the parent).
func (a *App) noteSubagentPhase(chatID string, before, after agentphase.Phase, ev ProviderRuntimeEvent) {
	var kind, key string
	switch {
	case before.InFlight() && (after.State == agentphase.Done || after.State == agentphase.Failed):
		kind, key = "done", "done:"+strconv.FormatInt(after.TurnEndedAt, 10)
	case isBlocked(after.State) && !isBlocked(before.State):
		kind, key = "blocked", "blocked:"+strconv.FormatInt(after.UpdatedAt, 10)
	default:
		return
	}
	if a.db == nil {
		return
	}
	c, err := a.chatByID(chatIDOf(chatID))
	if err != nil || c.ParentChatID == 0 || c.Kind == "side" || !a.subagentAutoDeliver() {
		return
	}
	summary := "" // done: the verb for a failed turn, blocked: the request
	switch {
	case kind == "blocked":
		summary = requestSummary(ev)
	case after.State == agentphase.Failed && after.Detail != "":
		summary = "failed (" + after.Detail + ")"
	case after.State == agentphase.Failed:
		summary = "failed"
	}
	if _, err := a.db.Exec(`INSERT OR IGNORE INTO subagent_notices (child_id, key, parent_id, kind, summary, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, key, c.ParentChatID, kind, summary, time.Now().UnixMilli()); err != nil {
		log.Printf("sub-agent notice for chat %d: %v", c.ID, err)
	}
}

func isBlocked(s agentphase.State) bool {
	return s == agentphase.WaitingInput || s == agentphase.WaitingApproval
}

// requestSummary is one line for what a blocked sub-agent wants: the tool or
// method, plus the question / command / path when the request carries one.
func requestSummary(ev ProviderRuntimeEvent) string {
	what := ""
	if qs, ok := ev.Input["questions"].([]any); ok && len(qs) > 0 {
		if q, ok := qs[0].(map[string]any); ok {
			what, _ = q["question"].(string)
		}
	}
	for _, k := range []string{"question", "command", "file_path", "path", "reason", "plan"} {
		if what != "" {
			break
		}
		what, _ = ev.Input[k].(string)
	}
	out := ev.Name
	if what != "" {
		out += ": " + what
	}
	return truncateRunes(out, 300, "…")
}

func truncateRunes(s string, n int, tail string) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + tail
	}
	return s
}

// deliverSubagentNotices sends every pending notice whose parent has settled,
// one per parent per pass (the first one's turn makes the next wait), and
// returns the delivered children's ids in order — one source of the delivery
// sweep (chatdelivery.go), run alone. send is ChatSend in production.
func (a *App) deliverSubagentNotices(now time.Time, send func(chatID int64, text string) error) []int64 {
	if a.db == nil {
		return nil
	}
	var delivered []int64
	a.runDeliveries(a.noticeDeliveries(now, func(c int64, t string, _ []string) error { return send(c, t) }, &delivered))
	return delivered
}

// noticeDeliveries offers every pending notice to its parent.
func (a *App) noticeDeliveries(now time.Time, send chatSender, delivered *[]int64) []pendingDelivery {
	_, _ = a.db.Exec(`DELETE FROM subagent_notices WHERE delivered_at != 0 AND delivered_at < ?`, now.Add(-subagentNoticeKeep).UnixMilli())

	type notice struct {
		childID, parentID int64
		key, kind, sum    string
		attempts          int
		created           int64
	}
	rows, err := a.db.Query(`SELECT child_id, key, parent_id, kind, summary, attempts, created_at FROM subagent_notices WHERE delivered_at = 0 ORDER BY created_at, child_id`)
	if err != nil {
		return nil
	}
	var pending []notice
	for rows.Next() {
		var n notice
		if rows.Scan(&n.childID, &n.key, &n.parentID, &n.kind, &n.sum, &n.attempts, &n.created) == nil {
			pending = append(pending, n)
		}
	}
	rows.Close()

	var out []pendingDelivery
	for i, n := range pending {
		n := n
		out = append(out, pendingDelivery{ChatID: n.parentID, Source: deliverNotice, Pos: int64(i), CreatedAt: n.created, deliver: func() bool {
			drop := func() {
				_, _ = a.db.Exec(`DELETE FROM subagent_notices WHERE child_id = ? AND key = ?`, n.childID, n.key)
			}
			parent, err := a.chatByID(n.parentID)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && parent.ArchivedAt != 0) {
				drop()
				return false
			}
			child, err := a.chatByID(n.childID)
			if errors.Is(err, sql.ErrNoRows) {
				drop()
				return false
			}
			if err != nil || !a.chatSettled(strconv.FormatInt(n.parentID, 10)) {
				return false // a parent turn is open: wait for it to settle
			}
			var text string
			markCollected := false
			switch n.kind {
			case "done":
				if child.CollectedAt != 0 {
					drop() // collect_results / wait_result already took it
					return false
				}
				text, markCollected = a.subagentDoneText(child, n.sum)
			case "blocked":
				if a.phases != nil && !isBlocked(a.phases.Get("chat:"+strconv.FormatInt(child.ID, 10)).State) {
					drop() // answered before the parent got to it
					return false
				}
				text = fmt.Sprintf("Sub-agent %s (#%d) is blocked: %s. Answer with `burrow chat-send --chat-id %d --text \"...\"` or tell the user.",
					subagentName(child), child.ID, n.sum, child.ID)
			}
			// Claim before sending: a crash or a failed mark after a successful
			// send must not hand the same notice to the next sweep.
			if _, err := a.db.Exec(`UPDATE subagent_notices SET delivered_at = ? WHERE child_id = ? AND key = ? AND delivered_at = 0`,
				now.UnixMilli(), n.childID, n.key); err != nil {
				return false
			}
			if err := send(n.parentID, text, nil); err != nil {
				if errors.Is(err, errChatBusy) {
					_, _ = a.db.Exec(`UPDATE subagent_notices SET delivered_at = 0 WHERE child_id = ? AND key = ?`, n.childID, n.key)
					return false
				}
				if n.attempts+1 >= subagentNoticeMaxAttempts {
					log.Printf("sub-agent notice %d/%s: dropped after %d attempts: %v", n.childID, n.key, n.attempts+1, err)
					drop()
					return true
				}
				_, _ = a.db.Exec(`UPDATE subagent_notices SET delivered_at = 0, attempts = attempts + 1 WHERE child_id = ? AND key = ?`, n.childID, n.key)
				return true
			}
			if markCollected {
				_ = a.MarkCollected(child.ID)
			}
			if delivered != nil {
				*delivered = append(*delivered, n.childID)
			}
			return true
		}})
	}
	return out
}

func subagentName(c Chat) string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return strconv.Quote(t)
	}
	return "chat"
}

// subagentDoneText is the completion prompt. A cut-off answer is left
// uncollected so collect_results can still hand over the whole of it.
func (a *App) subagentDoneText(child Chat, verb string) (text string, collected bool) {
	if verb == "" {
		verb = "finished"
	}
	answer, err := a.LastAssistantMessage(child.ID)
	if err != nil {
		answer = "(no final message)"
	}
	head := fmt.Sprintf("Sub-agent %s (#%d) %s:\n\n", subagentName(child), child.ID, verb)
	cut := truncateRunes(answer, subagentNoticeMaxChars, "")
	if cut == answer {
		return head + answer, true
	}
	return head + cut + fmt.Sprintf("\n\n[truncated — run `burrow collect-results` for the full answer of #%d]", child.ID), false
}
