package main

import (
	"fmt"
	"time"
)

// Snooze (ported in principle from t3code's thread snooze): the user parks a
// chat in Settled until a time, and it comes back on its own. chats.snoozed_until
// is read-only on the wire like collected_at — SaveChats never writes it, so a
// client that has never heard of it can't unsnooze a chat by saving.
//
// The wake rides chatScheduleLoop's 15 s sweep (no goroutine of its own): it
// clears the column, drops a manual settle pin, bumps last_activity_at so the
// chat floats to the top of the live list, and emits `chat-snooze-woke` for the
// desktop's notification.

// ChatSnooze sets (untilMs > 0) or clears (0) a chat's snooze.
func (a *App) ChatSnooze(chatID, untilMs int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if untilMs < 0 {
		untilMs = 0
	}
	res, err := a.db.Exec(`UPDATE chats SET snoozed_until = ? WHERE id = ?`, untilMs, chatID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat %d not found", chatID)
	}
	busEmit("chats-changed", nil)
	return nil
}

// wakeSnoozedChats wakes every chat whose snooze is due and returns their ids.
func (a *App) wakeSnoozedChats(now time.Time) []int64 {
	if a.db == nil {
		return nil
	}
	rows, err := a.db.Query(`SELECT id FROM chats WHERE snoozed_until != 0 AND snoozed_until <= ?`, now.UnixMilli())
	if err != nil {
		return nil
	}
	var due []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			due = append(due, id)
		}
	}
	rows.Close()

	var woke []int64
	for _, id := range due {
		// The snoozed_until guard makes a concurrent unsnooze/re-snooze win.
		res, err := a.db.Exec(`UPDATE chats SET snoozed_until = 0, settled_override = '', last_activity_at = ?
			WHERE id = ? AND snoozed_until != 0 AND snoozed_until <= ?`, now.UnixMilli(), id, now.UnixMilli())
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			woke = append(woke, id)
		}
	}
	if len(woke) > 0 {
		busEmit("chats-changed", nil)
		for _, id := range woke {
			busEmit("chat-snooze-woke", map[string]int64{"chat_id": id})
			a.pushChat(id, "snooze")
		}
	}
	return woke
}
