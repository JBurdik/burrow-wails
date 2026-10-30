package main

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Usage-limit resume (t3code's UsageLimitRecoveryWorker): a turn that fails on
// the provider's usage / rate limit records when the limit resets
// (chats.limit_reset_at, read-only on the wire). AgentChat shows a banner at
// the failed turn; [Resume automatically] arms it (chats.limit_resume_at), and
// the delivery pass sends "Continue where you left off." once that time has
// passed and the chat has settled.
//
// Detection is in the normalizer (turn.failed{usageLimit}, usage.limit hints
// from Claude's rate_limit_event); the reset time is parsed here, live only,
// because "resets 3pm" is relative to when it was said. Fallback: the
// exhausted window's reset from plan_usage (planusage.go).
//
// Loop guards: a reset already in the past at failure time is not recorded;
// the resume claims the row (UPDATE … WHERE limit_resume_at = ?) and stamps
// limit_resumed_key = chat:failedAt:resetAt, a key it never sends twice; any
// new running turn clears the limit. While a limit is active the chat's other
// deliveries (queue, schedules, notices) wait — each would fail on it.

const limitResumeText = "Continue where you left off."

// limitHints: the last rejected reset Claude's rate_limit_event reported per
// chat, consumed by the turn failure that follows it.
var limitHints sync.Map // chat id → unix ms

var (
	limitEpochRe = regexp.MustCompile(`\|(\d{10})\b`)
	// "resets 3pm (Europe/Prague)", "resets Nov 3, 9:30am", "try again at 3:45 PM",
	// "try again at Nov 3rd, 2025 9:00 AM".
	limitClockRe = regexp.MustCompile(`(?i)(?:resets|try again at)\s+(?:at\s+)?(?:([a-z]{3,9})\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(?:(\d{4}),?\s+)?(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*([ap]\.?m\.?)(?:\s*\(([^)]+)\))?`)
	// "(reset after 1h 2m 3s)", "try again in 2h 5m", "resets in 45 minutes".
	limitAfterRe = regexp.MustCompile(`(?i)(?:reset after|try again in|resets in)\s+((?:\d+\s*(?:d|days?|h|hours?|m|mins?|minutes?|s|secs?|seconds?)\s*)+)`)
	limitPartRe  = regexp.MustCompile(`(?i)(\d+)\s*(d|h|m|s)`)
)

// usageLimitReset reads the reset time out of a provider's limit message; ok
// is false when it names none. now anchors relative forms.
func usageLimitReset(text string, now time.Time) (time.Time, bool) {
	if m := limitEpochRe.FindStringSubmatch(text); m != nil {
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		return time.Unix(sec, 0), true
	}
	if m := limitAfterRe.FindStringSubmatch(text); m != nil {
		var d time.Duration
		for _, p := range limitPartRe.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			unit := map[string]time.Duration{"d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second}[strings.ToLower(p[2])]
			d += time.Duration(n) * unit
		}
		if d > 0 {
			return now.Add(d), true
		}
	}
	m := limitClockRe.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	loc := now.Location()
	if m[7] != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(m[7])); err == nil {
			loc = l
		}
	}
	hour, _ := strconv.Atoi(m[4])
	minute, _ := strconv.Atoi(m[5])
	if hour < 1 || hour > 12 || minute > 59 {
		return time.Time{}, false
	}
	hour %= 12
	if strings.HasPrefix(strings.ToLower(m[6]), "p") {
		hour += 12
	}
	local := now.In(loc)
	if m[1] != "" {
		mon, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:3]))
		day, _ := strconv.Atoi(m[2])
		if err != nil || day < 1 || day > 31 {
			return time.Time{}, false
		}
		year := local.Year()
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
		}
		t := time.Date(year, mon.Month(), day, hour, minute, 0, 0, loc)
		if m[3] == "" && t.Before(local.Add(-24*time.Hour)) {
			t = t.AddDate(1, 0, 0) // "Jan 2" said on Dec 30
		}
		return t, true
	}
	t := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !t.After(local) {
		t = t.AddDate(0, 0, 1)
	}
	return t, true
}

// isUsageLimitText: the provider's wording for a usage / rate limit (Claude
// "…usage limit reached|<epoch>", "You've hit your limit · resets 3pm",
// "5-hour limit reached ∙ resets 2am"; Codex "You've hit your usage limit…").
var usageLimitTextRe = regexp.MustCompile(`(?i)usage limit|hit your (?:usage )?limit|limit reached|rate_limit_error`)

func isUsageLimitText(s string) bool { return usageLimitTextRe.MatchString(s) }

// noteUsageLimitEvent is emitChatLine's hook for the two limit events.
func (a *App) noteUsageLimitEvent(chatID string, ev ProviderRuntimeEvent, now time.Time) {
	switch {
	case ev.Type == EvtUsageLimit && ev.LimitResetAt > 0:
		limitHints.Store(chatID, ev.LimitResetAt)
	case ev.Type == EvtTurnFailed && ev.UsageLimit:
		hint, _ := limitHints.LoadAndDelete(chatID)
		reset := ev.LimitResetAt
		if reset == 0 {
			if t, ok := usageLimitReset(ev.Message, now); ok {
				reset = t.UnixMilli()
			} else if h, ok := hint.(int64); ok {
				reset = h
			}
		}
		if reset != 0 {
			a.recordUsageLimit(chatIDOf(chatID), now, reset)
			return
		}
		go func() { // network: never on the stream path
			if r := a.planUsageReset(chatIDOf(chatID), now); r != 0 {
				a.recordUsageLimit(chatIDOf(chatID), now, r)
			}
		}()
	}
}

// planUsageReset is the fallback: the latest reset among the provider's
// exhausted windows (every one has to reset before a continuation can run).
func (a *App) planUsageReset(chatID int64, now time.Time) int64 {
	c, err := a.chatByID(chatID)
	if err != nil {
		return 0
	}
	kind := "claude"
	if chatTransport(c) == "codex-app-server" {
		kind = "codex"
	}
	var latest int64
	for _, p := range a.PlanUsage().Providers {
		if p.Kind != kind {
			continue
		}
		for _, r := range p.Rows {
			t, err := time.Parse(time.RFC3339, r.ResetsAt)
			if err == nil && r.Percent >= 99 && t.After(now) && t.UnixMilli() > latest {
				latest = t.UnixMilli()
			}
		}
	}
	return latest
}

// recordUsageLimit stores a detected limit. One already reset at failure time
// is ignored: arming it would resume into the same failure, again and again.
func (a *App) recordUsageLimit(chatID int64, failedAt time.Time, resetAt int64) {
	if a.db == nil || resetAt <= failedAt.UnixMilli() {
		return
	}
	if _, err := a.db.Exec(`UPDATE chats SET limit_reset_at = ?, limit_failed_at = ?, limit_resume_at = 0 WHERE id = ?`,
		resetAt, failedAt.UnixMilli(), chatID); err != nil {
		log.Printf("chat %d: record usage limit: %v", chatID, err)
		return
	}
	busEmit("chats-changed", nil)
}

// clearUsageLimitOnRunning: any new turn ends the limit episode.
func (a *App) clearUsageLimitOnRunning(chatID string) {
	res, err := a.db.Exec(`UPDATE chats SET limit_reset_at = 0, limit_resume_at = 0 WHERE id = ? AND (limit_reset_at != 0 OR limit_resume_at != 0)`, chatIDOf(chatID))
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		busEmit("chats-changed", nil)
	}
}

// ChatLimitArm is the banner's [Resume automatically]: resume at the reset.
func (a *App) ChatLimitArm(chatID int64) error {
	return a.limitWrite(`UPDATE chats SET limit_resume_at = limit_reset_at WHERE id = ? AND limit_reset_at != 0`, chatID)
}

// ChatLimitDismiss is the banner's [Dismiss]: forget the limit and any armed resume.
func (a *App) ChatLimitDismiss(chatID int64) error {
	return a.limitWrite(`UPDATE chats SET limit_reset_at = 0, limit_resume_at = 0 WHERE id = ?`, chatID)
}

func (a *App) limitWrite(q string, chatID int64) error {
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	res, err := a.db.Exec(q, chatID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat %d has no usage limit to resume", chatID)
	}
	busEmit("chats-changed", nil)
	return nil
}

// limitedChats: chats whose limit has not reset yet — their deliveries wait.
func (a *App) limitedChats(now time.Time) map[int64]bool {
	out := map[int64]bool{}
	rows, err := a.db.Query(`SELECT id FROM chats WHERE limit_reset_at > ?`, now.UnixMilli())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// limitResumeDeliveries offers every armed resume that is due.
func (a *App) limitResumeDeliveries(now time.Time, send chatSender, resumed *[]int64) []pendingDelivery {
	type row struct {
		id, resumeAt, resetAt, failedAt int64
		key                             string
	}
	rows, err := a.db.Query(`SELECT id, limit_resume_at, limit_reset_at, limit_failed_at, limit_resumed_key FROM chats
		WHERE limit_resume_at != 0 AND limit_resume_at <= ?`, now.UnixMilli())
	if err != nil {
		return nil
	}
	var due []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.resumeAt, &r.resetAt, &r.failedAt, &r.key) == nil {
			due = append(due, r)
		}
	}
	rows.Close()

	var out []pendingDelivery
	for _, r := range due {
		r := r
		out = append(out, pendingDelivery{ChatID: r.id, Source: deliverLimitResume, Pos: r.resumeAt, deliver: func() bool {
			if !a.chatSettled(strconv.FormatInt(r.id, 10)) {
				return false
			}
			key := fmt.Sprintf("%d:%d:%d", r.id, r.failedAt, r.resetAt)
			res, err := a.db.Exec(`UPDATE chats SET limit_resume_at = 0, limit_reset_at = 0, limit_resumed_key = ?
				WHERE id = ? AND limit_resume_at = ? AND limit_resumed_key != ?`, key, r.id, r.resumeAt, key)
			if err != nil {
				return false
			}
			if n, _ := res.RowsAffected(); n == 0 {
				// Disarmed meanwhile, or this very episode was already resumed.
				_, _ = a.db.Exec(`UPDATE chats SET limit_resume_at = 0 WHERE id = ? AND limit_resumed_key = ?`, r.id, key)
				return false
			}
			busEmit("chats-changed", nil)
			if err := send(r.id, limitResumeText, nil); err != nil {
				if errors.Is(err, errChatBusy) {
					_, _ = a.db.Exec(`UPDATE chats SET limit_resume_at = ?, limit_reset_at = ?, limit_resumed_key = '' WHERE id = ?`,
						r.resumeAt, r.resetAt, r.id)
					return false
				}
				log.Printf("chat %d: usage-limit resume not sent: %v", r.id, err)
				return true
			}
			if resumed != nil {
				*resumed = append(*resumed, r.id)
			}
			return true
		}})
	}
	return out
}
