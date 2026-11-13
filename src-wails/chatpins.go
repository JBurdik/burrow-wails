package main

import (
	"fmt"
	"strings"
	"time"
)

// Pins (threadbear's `pin` tool): an agent pins the few things the user
// should look at — a mockup, a preview URL, a PR — with a one-line note. They
// head the thread's Highlights surface in the Right Panel (HighlightsSurface).
// Agents add through the local `pin` verb; clients list and remove.

type ChatPin struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chat_id"`
	Target    string `json:"target"` // an absolute path or an http(s) URL
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
}

func chatPinsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_pins (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id    INTEGER NOT NULL,
			target     TEXT    NOT NULL,
			note       TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS chat_pins_chat ON chat_pins(chat_id)`,
	}
}

const maxPinsPerChat = 20

// AddPin implements control.Pins. Pinning the same target again updates its
// note instead of adding a twin.
func (a *App) AddPin(chatID int64, target, note string) (any, error) {
	if a.db == nil {
		return nil, fmt.Errorf("no database")
	}
	if _, err := a.chatByID(chatID); err != nil {
		return nil, err
	}
	target, note = strings.TrimSpace(target), strings.TrimSpace(note)
	if target == "" {
		return nil, fmt.Errorf("pin needs a target (a file path or an http(s) URL)")
	}
	res, err := a.db.Exec(`UPDATE chat_pins SET note = ?, created_at = ? WHERE chat_id = ? AND target = ?`, note, time.Now().UnixMilli(), chatID, target)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var count int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_pins WHERE chat_id = ?`, chatID).Scan(&count)
		if count >= maxPinsPerChat {
			return nil, fmt.Errorf("this thread already has %d pins: unpin some first (pins are for the few things that matter most)", maxPinsPerChat)
		}
		if _, err := a.db.Exec(`INSERT INTO chat_pins (chat_id, target, note, created_at) VALUES (?, ?, ?, ?)`, chatID, target, note, time.Now().UnixMilli()); err != nil {
			return nil, err
		}
	}
	busEmit("chat-pins-changed", map[string]int64{"chat_id": chatID})
	return map[string]any{"ok": true, "target": target}, nil
}

// ChatPinsList: a chat's pins, newest first.
func (a *App) ChatPinsList(chatID int64) ([]ChatPin, error) {
	out := []ChatPin{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(`SELECT id, chat_id, target, note, created_at FROM chat_pins WHERE chat_id = ? ORDER BY created_at DESC, id DESC`, chatID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ChatPin
		if rows.Scan(&p.ID, &p.ChatID, &p.Target, &p.Note, &p.CreatedAt) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// ChatPinRemove drops one pin (the surface's ×).
func (a *App) ChatPinRemove(id int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	var chatID int64
	if err := a.db.QueryRow(`SELECT chat_id FROM chat_pins WHERE id = ?`, id).Scan(&chatID); err != nil {
		return fmt.Errorf("pin %d: %w", id, err)
	}
	if _, err := a.db.Exec(`DELETE FROM chat_pins WHERE id = ?`, id); err != nil {
		return err
	}
	busEmit("chat-pins-changed", map[string]int64{"chat_id": chatID})
	return nil
}
