package main

import (
	"strings"
	"time"
)

// Read receipts shared by every client (desktop windows, tray panel, phone,
// PWA): reading a thread on one clears its unread dot on all of them.
//
// Keys are the client's own receipt keys — `chat:<id>` for a chat, the bare
// numeric pty id for a terminal leaf. Go stamps the time with ITS clock, the
// same clock as `turn_ended_at`, so a phone whose clock runs ahead can't
// pre-read the next turn. A receipt only ever moves forward (MAX), except an
// explicit clear (snooze wake).

func seenReceiptsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS seen_receipts (
			key     TEXT PRIMARY KEY,
			seen_at INTEGER NOT NULL
		)`,
	}
}

type seenReceipt struct {
	Key string `json:"key"`
	At  int64  `json:"at"` // unix ms; 0 = cleared
}

// SeenMark records that a client looked at `key` now and tells every client.
func (a *App) SeenMark(key string) (int64, error) {
	if key == "" || a.db == nil {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	if _, err := a.db.Exec(`INSERT INTO seen_receipts(key, seen_at) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET seen_at = MAX(seen_at, excluded.seen_at)`, key, now); err != nil {
		return 0, err
	}
	var at int64
	_ = a.db.QueryRow(`SELECT seen_at FROM seen_receipts WHERE key = ?`, key).Scan(&at)
	busEmit("seen-changed", seenReceipt{Key: key, At: at})
	return at, nil
}

// SeenList returns every stored receipt (a client merges it on start).
func (a *App) SeenList() ([]seenReceipt, error) {
	out := []seenReceipt{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(`SELECT key, seen_at FROM seen_receipts`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r seenReceipt
		if rows.Scan(&r.Key, &r.At) == nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// seenClear drops a receipt (a woken snooze must look unread again everywhere,
// including on a client that boots later and merges SeenList).
func (a *App) seenClear(key string) {
	if a.db == nil {
		return
	}
	if _, err := a.db.Exec(`DELETE FROM seen_receipts WHERE key = ?`, key); err == nil {
		busEmit("seen-changed", seenReceipt{Key: key, At: 0})
	}
}

// SeenMarkUnread rewinds a receipt to just before the latest finished turn
// (t3code's mark-unread), so every client shows it unread again. The one
// write that moves a receipt backwards — explicit, from the row menu.
func (a *App) SeenMarkUnread(key string) error {
	if key == "" || a.db == nil || a.phases == nil {
		return nil
	}
	phaseKey := key
	if !strings.HasPrefix(key, "chat:") {
		phaseKey = "pty:" + key // receipts key a pty by its bare id
	}
	ended := a.phases.Get(phaseKey).TurnEndedAt
	if ended <= 0 {
		return nil // nothing finished: nothing to be unread
	}
	at := ended - 1
	if _, err := a.db.Exec(`INSERT INTO seen_receipts(key, seen_at) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET seen_at = excluded.seen_at`, key, at); err != nil {
		return err
	}
	busEmit("seen-changed", seenReceipt{Key: key, At: at})
	return nil
}
