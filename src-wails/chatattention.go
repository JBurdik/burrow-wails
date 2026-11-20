package main

import "encoding/json"

// Thread attention: the per-chat facts the Sidebar, tray panel, phone feed and
// iOS app sort threads by. They used to be derived in each client (TS
// settledFor, Swift AppModel.settled/needsYou/quietDone) and drifted; Go
// computes them once onto every list_chats row. What stays client-side is what
// differs per device: the read receipt and whether the chat is on screen
// (displayStatus), plus the in-flight gate below.

// autoSettleAfterMs: a thread with no activity for 3 days settles by itself
// (t3code's `sidebarAutoSettleAfterDays ?? 3`).
const autoSettleAfterMs = 3 * 24 * 60 * 60 * 1000

type ChatAttention struct {
	// Unix ms from which the thread counts as settled, 0 = not settled by any
	// rule. 1 = settled now (snoozed, or settled by hand); otherwise the
	// quiet-for-3-days moment. A client shows it settled when its phase is not
	// in flight and now >= settled_since — a timestamp rather than a bool, so
	// the 3-day rule flips on the client's clock with no event from Go. A snooze
	// ending is not a clock flip here: chatScheduleLoop clears snoozed_until
	// and emits chats-changed.
	SettledSince int64 `json:"settled_since"`
	// The last turn's end_turn said needs=you (a decision is waiting).
	NeedsYou bool `json:"needs_you"`
	// The last turn's end_turn said needs=nothing: an unread finish draws a
	// tick instead of a dot.
	SaidDone bool `json:"said_done"`
}

// chatAttentionFor is pure. Precedence is settledFor's: snooze, then the
// manual pin (both directions), then a pending ask, then the 3-day clock. The
// in-flight check (a running turn is never settled) stays with the phase on
// the client, since a phase change emits no chats-changed.
func chatAttentionFor(c Chat, now int64) ChatAttention {
	var tn struct {
		Needs string `json:"needs"`
	}
	if c.TurnNeeds != "" {
		_ = json.Unmarshal([]byte(c.TurnNeeds), &tn)
	}
	at := ChatAttention{NeedsYou: tn.Needs == "you", SaidDone: tn.Needs == "nothing"}
	switch {
	case c.SnoozedUntil > now, c.SettledOverride == "settled":
		at.SettledSince = 1
	case c.SettledOverride == "active", at.NeedsYou:
	default:
		// max(…, 1): a row with no timestamp (migrated, 0) is settled since
		// the epoch, and 0 must keep meaning "never".
		at.SettledSince = max(c.LastActivityAt+autoSettleAfterMs, 1)
	}
	return at
}
