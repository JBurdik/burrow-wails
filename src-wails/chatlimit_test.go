package main

import (
	"strconv"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

// Captured provider lines → a usage-limit turn failure (or hint), and the
// reset time read out of its wording.
func TestUsageLimitFromCapturedLines(t *testing.T) {
	prague, _ := time.LoadLocation("Europe/Prague")
	now := time.Date(2026, 10, 30, 13, 20, 0, 0, prague)
	cases := []struct {
		name, kind, line string
		want             time.Time // zero = no reset in the text
	}{
		{"claude epoch", "claude-data",
			`{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached|1793368800","session_id":"s"}`,
			time.Unix(1793368800, 0)},
		{"claude resets tz", "claude-data",
			`{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit · resets 3pm (Europe/Prague)"}`,
			time.Date(2026, 10, 30, 15, 0, 0, 0, prague)},
		{"claude 5-hour tomorrow", "claude-data",
			`{"type":"result","subtype":"success","is_error":true,"result":"5-hour limit reached ∙ resets 1:15am"}`,
			time.Date(2026, 10, 31, 1, 15, 0, 0, prague)},
		{"claude 429 gateway", "claude-data",
			`{"type":"result","subtype":"success","is_error":true,"api_error_status":429,"result":"API Error: 429 {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\"}} (reset after 1h 30m)"}`,
			now.Add(90 * time.Minute)},
		{"codex", "acp-data",
			`{"_burrow":"turn","error":"You've hit your usage limit. Upgrade to Pro (https://openai.com/chatgpt/pricing) or try again at Nov 3rd, 2026 9:05 AM."}`,
			time.Date(2026, 11, 3, 9, 5, 0, 0, prague)},
		{"codex no time", "acp-data", `{"_burrow":"turn","error":"You've hit your usage limit."}`, time.Time{}},
	}
	for _, c := range cases {
		evs := NormalizeChatLine(c.kind, c.line, 1)
		if len(evs) != 1 || evs[0].Type != EvtTurnFailed || !evs[0].UsageLimit {
			t.Fatalf("%s: events %+v, want one usage-limit turn.failed", c.name, evs)
		}
		got, ok := usageLimitReset(evs[0].Message, now)
		if c.want.IsZero() {
			if ok {
				t.Fatalf("%s: reset %v, want none", c.name, got)
			}
			continue
		}
		if !ok || !got.Equal(c.want) {
			t.Fatalf("%s: reset %v (%v), want %v", c.name, got, ok, c.want)
		}
	}

	hint := NormalizeChatLine("claude-data", `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1793368800,"rateLimitType":"five_hour"}}`, 1)
	if len(hint) != 1 || hint[0].Type != EvtUsageLimit || hint[0].LimitResetAt != 1793368800000 {
		t.Fatalf("rate_limit_event → %+v", hint)
	}
	if evs := NormalizeChatLine("claude-data", `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1}}`, 1); len(evs) != 0 {
		t.Fatalf("a warning is not a limit: %+v", evs)
	}
	// An ordinary error result and an ordinary turn failure stay what they were.
	if evs := NormalizeChatLine("claude-data", `{"type":"result","subtype":"success","is_error":false,"result":"ok"}`, 1); evs[0].Type != EvtTurnCompleted {
		t.Fatalf("plain result → %+v", evs)
	}
	if evs := NormalizeChatLine("acp-data", `{"_burrow":"turn","error":"boom"}`, 1); evs[0].UsageLimit {
		t.Fatalf("plain failure marked as a limit: %+v", evs)
	}
}

func chatRow(t *testing.T, a *App, id int64) Chat {
	t.Helper()
	c, err := a.chatByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Armed: nothing goes out before the reset or into a running turn, the queue
// waits behind the limit, then exactly one "Continue" — never twice for the
// same episode. A reset already past at failure time is not recorded.
func TestUsageLimitResumesOnce(t *testing.T) {
	a, ws := noticeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	id := strconv.FormatInt(c.ID, 10)
	now := time.Now()

	a.noteUsageLimitEvent(id, ProviderRuntimeEvent{Type: EvtTurnFailed, UsageLimit: true, Message: "usage limit reached|" + strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)}, now)
	if chatRow(t, a, c.ID).LimitResetAt != 0 {
		t.Fatal("a reset already in the past was recorded (would loop)")
	}

	reset := now.Add(time.Hour)
	a.noteUsageLimitEvent(id, ProviderRuntimeEvent{Type: EvtUsageLimit, LimitResetAt: reset.UnixMilli()}, now)
	a.noteUsageLimitEvent(id, ProviderRuntimeEvent{Type: EvtTurnFailed, UsageLimit: true, Message: "You've hit your limit"}, now)
	if got := chatRow(t, a, c.ID).LimitResetAt; got != reset.UnixMilli() {
		t.Fatalf("limit_reset_at = %d, want the rate_limit_event hint %d", got, reset.UnixMilli())
	}
	if err := a.ChatLimitArm(c.ID); err != nil {
		t.Fatal(err)
	}
	if got := chatRow(t, a, c.ID).LimitResumeAt; got != reset.UnixMilli() {
		t.Fatalf("limit_resume_at = %d", got)
	}
	a.ChatQueueAdd(c.ID, "queued", nil)

	s := &imgSink{}
	a.deliverPending(now, s.send) // limit not reset yet: nothing, queue included
	a.phases.Apply(chatKey(c.ID), agentphase.Event{Kind: agentphase.HookRunning})
	a.deliverPending(reset.Add(time.Second), s.send) // due, but a turn is open
	if len(s.text) != 0 {
		t.Fatalf("sent %q too early", s.text)
	}
	a.phases.Apply(chatKey(c.ID), agentphase.Event{Kind: agentphase.HookDone})
	a.deliverPending(reset.Add(time.Second), s.send)
	a.deliverPending(reset.Add(time.Second), s.send)
	if len(s.text) != 2 || s.text[0] != limitResumeText || s.text[1] != "queued" {
		t.Fatalf("sent %q, want resume then the queue", s.text)
	}
	if r := chatRow(t, a, c.ID); r.LimitResetAt != 0 || r.LimitResumeAt != 0 {
		t.Fatalf("limit not cleared: %+v", r)
	}

	// The same episode re-armed behind the sweep's back is not resumed again.
	failedAt := now.UnixMilli()
	if _, err := a.db.Exec(`UPDATE chats SET limit_reset_at = ?, limit_resume_at = ?, limit_failed_at = ? WHERE id = ?`,
		reset.UnixMilli(), reset.UnixMilli(), failedAt, c.ID); err != nil {
		t.Fatal(err)
	}
	a.deliverPending(reset.Add(time.Minute), s.send)
	if len(s.text) != 2 {
		t.Fatalf("resumed the same episode twice: %q", s.text)
	}

	// Dismiss forgets it; arming without a limit is refused.
	_ = a.ChatLimitDismiss(c.ID)
	if err := a.ChatLimitArm(c.ID); err == nil {
		t.Fatal("armed a chat with no limit")
	}
}
