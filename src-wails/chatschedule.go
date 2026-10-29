package main

import (
	"database/sql"
	"errors"
	"log"
	"strconv"
	"time"

	"burrow/internal/control"
)

// Scheduled prompts (`burrow schedule`): a message for a chat, sent later. A
// wake-up is the same thing with "Wake-up: <reason>" as the text.
//
// A table, not a timer: an item survives an app restart, and one missed while
// the app was closed fires on the first sweep after launch. Delivery is
// ChatSend — the one door for a prompt, which also starts (resumes) a stopped
// or slept process — but only once the chat's turn has settled: a ChatSend
// into a running turn is not queued, it is a steer (Claude takes the stdin line
// at the next tool boundary; Codex would race a second turn/start). So a due
// item waits for idle/done/failed/stale, one item per chat per sweep (the
// first one's turn makes the next wait its turn).

const (
	chatScheduleSweepInterval = 15 * time.Second
	// A failed delivery (agent wouldn't start) is retried this many times,
	// chatScheduleRetryDelay apart, then dropped with a log line.
	chatScheduleMaxAttempts = 5
	chatScheduleRetryDelay  = time.Minute
)

func chatSchedulesSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_schedules (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id    INTEGER NOT NULL,
			text       TEXT    NOT NULL,
			due_at     INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			attempts   INTEGER NOT NULL DEFAULT 0,
			last_error TEXT    NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS chat_schedules_due ON chat_schedules(due_at)`,
	}
}

// scheduleAdapter is the App behind control's Schedules — unexported methods,
// so they don't become Wails bindings / remote commands.
type scheduleAdapter struct{ a *App }

func (s scheduleAdapter) Add(chatID int64, text string, due time.Time) (control.ScheduledPrompt, error) {
	a := s.a
	if _, err := a.chatByID(chatID); err != nil {
		return control.ScheduledPrompt{}, err
	}
	now := time.Now()
	res, err := a.db.Exec(`INSERT INTO chat_schedules (chat_id, text, due_at, created_at) VALUES (?, ?, ?, ?)`,
		chatID, text, due.UnixMilli(), now.UnixMilli())
	if err != nil {
		return control.ScheduledPrompt{}, err
	}
	id, _ := res.LastInsertId()
	return control.ScheduledPrompt{ID: id, ChatID: chatID, Text: text,
		DueAt: time.UnixMilli(due.UnixMilli()), CreatedAt: time.UnixMilli(now.UnixMilli())}, nil
}

func (s scheduleAdapter) List(chatID int64) ([]control.ScheduledPrompt, error) {
	q := `SELECT id, chat_id, text, due_at, created_at, attempts, last_error FROM chat_schedules`
	args := []any{}
	if chatID > 0 {
		q += ` WHERE chat_id = ?`
		args = append(args, chatID)
	}
	rows, err := s.a.db.Query(q+` ORDER BY due_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []control.ScheduledPrompt{}
	for rows.Next() {
		var p control.ScheduledPrompt
		var due, created int64
		if err := rows.Scan(&p.ID, &p.ChatID, &p.Text, &due, &created, &p.Attempts, &p.LastError); err != nil {
			return nil, err
		}
		p.DueAt, p.CreatedAt = time.UnixMilli(due), time.UnixMilli(created)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s scheduleAdapter) Cancel(id int64) (bool, error) {
	res, err := s.a.db.Exec(`DELETE FROM chat_schedules WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// chatSettled: no turn open — neither by the phase nor by a Codex/ACP turn the
// transport still holds (the same two tests auto-sleep uses).
func (a *App) chatSettled(id string) bool {
	if a.phases != nil && a.phases.Get("chat:"+id).InFlight() {
		return false
	}
	if sess, live := a.acpReg().get(id); live {
		sess.mu.Lock()
		busy := sess.pendingTurn != 0
		sess.mu.Unlock()
		if busy {
			return false
		}
	}
	return true
}

// fireDueSchedules delivers every item due at `now` whose chat has settled and
// returns the delivered ids. send is ChatSend in production.
func (a *App) fireDueSchedules(now time.Time, send func(chatID int64, text string) error) []int64 {
	if a.db == nil {
		return nil
	}
	type item struct {
		id, chatID int64
		text       string
		attempts   int
	}
	rows, err := a.db.Query(`SELECT id, chat_id, text, attempts FROM chat_schedules WHERE due_at <= ? ORDER BY due_at, id`, now.UnixMilli())
	if err != nil {
		return nil
	}
	var due []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.chatID, &it.text, &it.attempts) == nil {
			due = append(due, it)
		}
	}
	rows.Close()

	var fired []int64
	sent := map[int64]bool{}
	for _, it := range due {
		if sent[it.chatID] {
			continue
		}
		if _, err := a.chatByID(it.chatID); errors.Is(err, sql.ErrNoRows) {
			_, _ = a.db.Exec(`DELETE FROM chat_schedules WHERE chat_id = ?`, it.chatID) // chat deleted
			continue
		}
		if !a.chatSettled(strconv.FormatInt(it.chatID, 10)) {
			continue // a turn is open: wait for it to settle
		}
		sent[it.chatID] = true
		if err := send(it.chatID, it.text); err != nil {
			if it.attempts+1 >= chatScheduleMaxAttempts {
				log.Printf("schedule %d: dropped after %d attempts: %v", it.id, it.attempts+1, err)
				_, _ = a.db.Exec(`DELETE FROM chat_schedules WHERE id = ?`, it.id)
				continue
			}
			_, _ = a.db.Exec(`UPDATE chat_schedules SET attempts = attempts + 1, last_error = ?, due_at = ? WHERE id = ?`,
				err.Error(), now.Add(chatScheduleRetryDelay).UnixMilli(), it.id)
			continue
		}
		_, _ = a.db.Exec(`DELETE FROM chat_schedules WHERE id = ?`, it.id)
		fired = append(fired, it.id)
	}
	return fired
}

func (a *App) chatScheduleLoop() {
	send := func(chatID int64, text string) error { return a.ChatSend(chatID, text, nil) }
	for range time.Tick(chatScheduleSweepInterval) {
		a.fireDueSchedules(time.Now(), send)
	}
}
