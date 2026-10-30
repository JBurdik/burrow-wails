package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"
)

// The user's queued follow-ups (a message typed while a turn runs) live here,
// not in the client's transcript: a client-side queue only drained while some
// desktop view had the chat mounted, so nothing went out with the desktop
// closed or the phone asleep. Go drains it in the settled-chat delivery sweep
// (chatdelivery.go), behind sub-agent notices and ahead of scheduled prompts.
//
// Idempotency: delivery claims an item by DELETING it in the same transaction
// that reads it — once gone, no other sweep, kick or steer can send it again.
// A failed send puts it back at the head under its old id. At-most-once: a
// crash between the claim and the send loses that one message.
//
// ord is the position; reorder rewrites them 0..n-1, a requeue goes below the
// head. chat-queue-changed carries the chat's whole queue (state, not delta).

// chatQueueMaxAttempts: a head whose send failed this often stops the
// automatic drain for that chat (the agent won't start). It stays visible and
// is retried after an edit (which resets the count).
const chatQueueMaxAttempts = 5

func chatQueueSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_queue (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id     INTEGER NOT NULL,
			ord         INTEGER NOT NULL,
			text        TEXT    NOT NULL,
			attachments TEXT    NOT NULL DEFAULT '[]',
			created_at  INTEGER NOT NULL,
			attempts    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS chat_queue_chat ON chat_queue(chat_id, ord)`,
	}
}

type ChatQueueItem struct {
	ID          int64    `json:"id"`
	ChatID      int64    `json:"chatId"`
	Ord         int64    `json:"ord"`
	Text        string   `json:"text"`
	Attachments []string `json:"attachments"`
	CreatedAt   int64    `json:"createdAt"`
	Attempts    int      `json:"attempts"`
}

type chatQueueChanged struct {
	ChatID int64           `json:"chatId"`
	Items  []ChatQueueItem `json:"items"`
}

func scanQueueItem(row interface{ Scan(...any) error }) (ChatQueueItem, error) {
	var it ChatQueueItem
	var att string
	if err := row.Scan(&it.ID, &it.ChatID, &it.Ord, &it.Text, &att, &it.CreatedAt, &it.Attempts); err != nil {
		return it, err
	}
	if json.Unmarshal([]byte(att), &it.Attachments) != nil || it.Attachments == nil {
		it.Attachments = []string{}
	}
	return it, nil
}

const chatQueueColumns = `id, chat_id, ord, text, attachments, created_at, attempts`

// ChatQueueList is the chat's queue, head first.
func (a *App) ChatQueueList(chatID int64) ([]ChatQueueItem, error) {
	if a.db == nil {
		return nil, errors.New("no database")
	}
	rows, err := a.db.Query(`SELECT `+chatQueueColumns+` FROM chat_queue WHERE chat_id = ? ORDER BY ord, id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatQueueItem{}
	for rows.Next() {
		it, err := scanQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (a *App) emitQueueChanged(chatID int64) {
	items, err := a.ChatQueueList(chatID)
	if err != nil {
		return
	}
	busEmit("chat-queue-changed", chatQueueChanged{ChatID: chatID, Items: items})
}

// ChatQueueAdd appends a follow-up at the tail. A chat that has already
// settled (the client thought it busy, the turn ended meanwhile) is drained
// right away instead of on the next sweep.
func (a *App) ChatQueueAdd(chatID int64, text string, images []string) (ChatQueueItem, error) {
	if _, err := a.chatByID(chatID); err != nil {
		return ChatQueueItem{}, err
	}
	if images == nil {
		images = []string{}
	}
	att, _ := json.Marshal(images)
	now := time.Now().UnixMilli()
	res, err := a.db.Exec(`INSERT INTO chat_queue (chat_id, ord, text, attachments, created_at)
		VALUES (?, (SELECT COALESCE(MAX(ord), -1) + 1 FROM chat_queue WHERE chat_id = ?), ?, ?, ?)`,
		chatID, chatID, text, string(att), now)
	if err != nil {
		return ChatQueueItem{}, err
	}
	id, _ := res.LastInsertId()
	it, err := scanQueueItem(a.db.QueryRow(`SELECT `+chatQueueColumns+` FROM chat_queue WHERE id = ?`, id))
	if err != nil {
		return ChatQueueItem{}, err
	}
	a.emitQueueChanged(chatID)
	a.kickChatDelivery()
	return it, nil
}

var errNotQueued = errors.New("not in the queue (already sent or removed)")

// ChatQueueUpdate rewrites a queued item's text in place; resets its failed
// attempts so an edited head drains again.
func (a *App) ChatQueueUpdate(id int64, text string) error {
	return a.queueWrite(id, `UPDATE chat_queue SET text = ?, attempts = 0 WHERE id = ?`, text, id)
}

// ChatQueueRemove drops one queued item. errNotQueued when it is gone — the
// steer path relies on that: a removed item can't also be drained.
func (a *App) ChatQueueRemove(id int64) error {
	return a.queueWrite(id, `DELETE FROM chat_queue WHERE id = ?`, id)
}

func (a *App) queueWrite(id int64, q string, args ...any) error {
	if a.db == nil {
		return errors.New("no database")
	}
	var chatID int64
	if err := a.db.QueryRow(`SELECT chat_id FROM chat_queue WHERE id = ?`, id).Scan(&chatID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotQueued
		}
		return err
	}
	res, err := a.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotQueued
	}
	a.emitQueueChanged(chatID)
	return nil
}

// ChatQueueReorder sets the order to ids (head first). Ids not in the queue
// any more are ignored; queued ones missing from ids keep their relative
// order after the listed ones.
func (a *App) ChatQueueReorder(chatID int64, ids []int64) error {
	items, err := a.ChatQueueList(chatID)
	if err != nil {
		return err
	}
	pos := map[int64]int{}
	for i, id := range ids {
		pos[id] = i
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	next := len(ids)
	for _, it := range items {
		ord, ok := pos[it.ID]
		if !ok {
			ord = next
			next++
		}
		if _, err := tx.Exec(`UPDATE chat_queue SET ord = ? WHERE id = ?`, ord, it.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.emitQueueChanged(chatID)
	return nil
}

// claimQueueItem deletes the item and returns it, in one transaction: only
// one caller ever gets a given item.
func (a *App) claimQueueItem(id int64) (ChatQueueItem, bool) {
	tx, err := a.db.Begin()
	if err != nil {
		return ChatQueueItem{}, false
	}
	defer tx.Rollback()
	it, err := scanQueueItem(tx.QueryRow(`SELECT `+chatQueueColumns+` FROM chat_queue WHERE id = ?`, id))
	if err != nil {
		return ChatQueueItem{}, false
	}
	if _, err := tx.Exec(`DELETE FROM chat_queue WHERE id = ?`, id); err != nil {
		return ChatQueueItem{}, false
	}
	return it, tx.Commit() == nil
}

// requeueHead puts a claimed item back in front of the queue under its old id.
func (a *App) requeueHead(it ChatQueueItem, attempts int) {
	att, _ := json.Marshal(it.Attachments)
	if _, err := a.db.Exec(`INSERT INTO chat_queue (id, chat_id, ord, text, attachments, created_at, attempts)
		VALUES (?, ?, (SELECT COALESCE(MIN(ord), 1) - 1 FROM chat_queue WHERE chat_id = ?), ?, ?, ?, ?)`,
		it.ID, it.ChatID, it.ChatID, it.Text, string(att), it.CreatedAt, attempts); err != nil {
		log.Printf("chat %d: requeue of queued message %d failed: %v", it.ChatID, it.ID, err)
	}
}

// queueDeliveries offers each chat's queue head. Only the head: a stuck head
// (failed too often) holds the rest back rather than sending out of order.
func (a *App) queueDeliveries(send chatSender, delivered *[]int64) []pendingDelivery {
	rows, err := a.db.Query(`SELECT ` + chatQueueColumns + ` FROM chat_queue ORDER BY chat_id, ord, id`)
	if err != nil {
		return nil
	}
	var heads []ChatQueueItem
	for rows.Next() {
		it, err := scanQueueItem(rows)
		if err == nil && (len(heads) == 0 || heads[len(heads)-1].ChatID != it.ChatID) {
			heads = append(heads, it)
		}
	}
	rows.Close()

	var out []pendingDelivery
	for _, head := range heads {
		if head.Attempts >= chatQueueMaxAttempts {
			continue
		}
		head := head
		out = append(out, pendingDelivery{ChatID: head.ChatID, Source: deliverQueue, Pos: head.Ord, CreatedAt: head.CreatedAt,
			deliver: func() bool {
				if _, err := a.chatByID(head.ChatID); errors.Is(err, sql.ErrNoRows) {
					_, _ = a.db.Exec(`DELETE FROM chat_queue WHERE chat_id = ?`, head.ChatID) // chat deleted
					return false
				}
				if !a.chatSettled(strconv.FormatInt(head.ChatID, 10)) {
					return false
				}
				it, ok := a.claimQueueItem(head.ID)
				if !ok {
					return false // removed / steered meanwhile
				}
				a.emitQueueChanged(it.ChatID)
				if err := send(it.ChatID, it.Text, it.Attachments); err != nil {
					busy := errors.Is(err, errChatBusy)
					attempts := it.Attempts
					if !busy {
						attempts++
						log.Printf("chat %d: queued message %d not sent (attempt %d): %v", it.ChatID, it.ID, attempts, err)
					}
					a.requeueHead(it, attempts)
					a.emitQueueChanged(it.ChatID)
					return !busy
				}
				if delivered != nil {
					*delivered = append(*delivered, it.ID)
				}
				return true
			}})
	}
	return out
}
