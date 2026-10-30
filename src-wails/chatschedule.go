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

// Recurring tasks (`--every` / `--weekly`, Settings → Scheduled) are the same
// rows with a rule: a run reschedules the row to control.NextDue(rule, now)
// instead of deleting it. "now", not the missed slot — an app that was closed
// (or asleep) through several slots runs once on the first sweep and then
// waits for the next one. A run can target a NEW chat per run (new_chat: a
// fresh row in workspace_id with agent, titled "<name> · <date>"): nothing to
// wait for, so it is not held back by any turn.
func chatSchedulesAlters() []string {
	return []string{
		`ALTER TABLE chat_schedules ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_schedules ADD COLUMN kind TEXT NOT NULL DEFAULT 'once'`,
		`ALTER TABLE chat_schedules ADD COLUMN every_min INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chat_schedules ADD COLUMN weekdays TEXT NOT NULL DEFAULT ''`, // "1,3"
		`ALTER TABLE chat_schedules ADD COLUMN time_of_day TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_schedules ADD COLUMN new_chat INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chat_schedules ADD COLUMN workspace_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chat_schedules ADD COLUMN agent TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_schedules ADD COLUMN paused INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chat_schedules ADD COLUMN last_run_at INTEGER NOT NULL DEFAULT 0`,
	}
}

const scheduleCols = `id, chat_id, name, text, due_at, created_at, attempts, last_error,
	kind, every_min, weekdays, time_of_day, new_chat, workspace_id, agent, paused, last_run_at`

func scanSchedule(row interface{ Scan(...any) error }) (control.ScheduledPrompt, error) {
	var p control.ScheduledPrompt
	var due, created, lastRun int64
	var days string
	err := row.Scan(&p.ID, &p.ChatID, &p.Name, &p.Text, &due, &created, &p.Attempts, &p.LastError,
		&p.Rule.Kind, &p.Rule.EveryMin, &days, &p.Rule.TimeOfDay, &p.NewChat, &p.WorkspaceID, &p.Agent, &p.Paused, &lastRun)
	if err != nil {
		return p, err
	}
	for _, d := range strings.Split(days, ",") {
		if n, err := strconv.Atoi(d); err == nil {
			p.Rule.Weekdays = append(p.Rule.Weekdays, n)
		}
	}
	p.DueAt, p.CreatedAt, p.Recurs = time.UnixMilli(due), time.UnixMilli(created), p.Rule.Describe()
	if lastRun > 0 {
		t := time.UnixMilli(lastRun)
		p.LastRunAt = &t
	}
	return p, nil
}

func weekdaysCol(days []int) string {
	s := make([]string, len(days))
	for i, d := range days {
		s[i] = strconv.Itoa(d)
	}
	return strings.Join(s, ",")
}

// scheduleAdapter is the App behind control's Schedules — unexported methods,
// so they don't become Wails bindings / remote commands.
type scheduleAdapter struct{ a *App }

func (s scheduleAdapter) Add(p control.ScheduledPrompt) (control.ScheduledPrompt, error) {
	p.ID = 0
	return s.save(p)
}

// save inserts (ID 0) or rewrites an item. A recurring item without a due
// time gets the rule's next slot; an edit that keeps the rule keeps its slot.
func (s scheduleAdapter) save(p control.ScheduledPrompt) (control.ScheduledPrompt, error) {
	a := s.a
	var none control.ScheduledPrompt
	p.Text = strings.TrimSpace(p.Text)
	if p.Text == "" {
		return none, fmt.Errorf("schedule needs text")
	}
	if p.Rule.Kind == "" {
		p.Rule.Kind = "once"
	}
	if err := p.Rule.Validate(); err != nil {
		return none, err
	}
	if p.NewChat {
		if p.WorkspaceID <= 0 && p.ChatID > 0 {
			c, err := a.chatByID(p.ChatID)
			if err != nil {
				return none, err
			}
			p.WorkspaceID = c.WorkspaceID
		}
		if p.WorkspaceID <= 0 {
			return none, fmt.Errorf("a new-chat schedule needs a workspace")
		}
		p.ChatID = 0
	} else {
		if _, err := a.chatByID(p.ChatID); err != nil {
			return none, err
		}
		p.WorkspaceID, p.Agent = 0, ""
	}
	if p.Name = strings.TrimSpace(p.Name); p.Name == "" {
		p.Name = subagentTitle(p.Text)
	}
	now := time.Now()
	var old control.ScheduledPrompt
	if p.ID > 0 {
		var err error
		if old, err = s.get(p.ID); err != nil {
			return none, err
		}
		if p.Rule.Recurring() && p.DueAt.IsZero() && old.Rule.Kind == p.Rule.Kind && old.Rule.Describe() == p.Rule.Describe() {
			p.DueAt = old.DueAt
		}
	}
	if p.DueAt.IsZero() {
		next, ok := control.NextDue(p.Rule, now, time.Local)
		if !ok {
			return none, fmt.Errorf("schedule needs a time")
		}
		p.DueAt = next
	}
	vals := []any{p.ChatID, p.Name, p.Text, p.DueAt.UnixMilli(), p.Rule.Kind, p.Rule.EveryMin, weekdaysCol(p.Rule.Weekdays),
		p.Rule.TimeOfDay, p.NewChat, p.WorkspaceID, p.Agent, p.Paused}
	if p.ID > 0 {
		_, err := a.db.Exec(`UPDATE chat_schedules SET chat_id = ?, name = ?, text = ?, due_at = ?, kind = ?, every_min = ?,
			weekdays = ?, time_of_day = ?, new_chat = ?, workspace_id = ?, agent = ?, paused = ?, attempts = 0, last_error = ''
			WHERE id = ?`, append(vals, p.ID)...)
		if err != nil {
			return none, err
		}
		return s.get(p.ID)
	}
	res, err := a.db.Exec(`INSERT INTO chat_schedules (chat_id, name, text, due_at, kind, every_min, weekdays, time_of_day,
		new_chat, workspace_id, agent, paused, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(vals, now.UnixMilli())...)
	if err != nil {
		return none, err
	}
	id, _ := res.LastInsertId()
	return s.get(id)
}

func (s scheduleAdapter) get(id int64) (control.ScheduledPrompt, error) {
	p, err := scanSchedule(s.a.db.QueryRow(`SELECT `+scheduleCols+` FROM chat_schedules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("no pending schedule %d", id)
	}
	return p, err
}

func (s scheduleAdapter) List(chatID int64) ([]control.ScheduledPrompt, error) {
	q := `SELECT ` + scheduleCols + ` FROM chat_schedules`
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
		p, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
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

// SetPaused pauses or resumes. A recurring item resumed past its slot moves to
// the next one — the runs missed while paused are skipped, not caught up.
func (s scheduleAdapter) SetPaused(id int64, paused bool) (control.ScheduledPrompt, error) {
	p, err := s.get(id)
	if err != nil {
		return p, err
	}
	due := p.DueAt
	if now := time.Now(); !paused && p.Rule.Recurring() && due.Before(now) {
		due, _ = control.NextDue(p.Rule, now, time.Local)
	}
	if _, err := s.a.db.Exec(`UPDATE chat_schedules SET paused = ?, due_at = ? WHERE id = ?`, paused, due.UnixMilli(), id); err != nil {
		return p, err
	}
	return s.get(id)
}

func (s scheduleAdapter) RunNow(id int64) (control.ScheduledPrompt, error) {
	p, err := s.get(id)
	if err != nil {
		return p, err
	}
	if p.Paused {
		return p, fmt.Errorf("schedule %d is paused — resume it first", id)
	}
	if _, err := s.a.db.Exec(`UPDATE chat_schedules SET due_at = ?, attempts = 0 WHERE id = ?`, time.Now().UnixMilli(), id); err != nil {
		return p, err
	}
	s.a.kickChatDelivery()
	return s.get(id)
}

// startScheduledChat is a new-chat run's row: the item's agent (else the
// default chat agent, else the first enabled one) in its workspace.
func (a *App) startScheduledChat(p control.ScheduledPrompt, now time.Time) (Chat, error) {
	var cfg spawnConfig
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	var inst *providerInstance
	if p.Agent != "" {
		if inst = providerByIDOrName(p.Agent, cfg.Providers, cfg.Aliases); inst == nil {
			return Chat{}, fmt.Errorf("no agent named %q", p.Agent)
		}
	} else {
		inst = providerByIDOrName(firstNonEmpty(cfg.UI.DefaultChatAgent, "claude"), cfg.Providers, cfg.Aliases)
		for i := range cfg.Providers {
			if inst == nil && cfg.Providers[i].Enabled {
				inst = &cfg.Providers[i]
			}
		}
	}
	if inst == nil {
		return Chat{}, fmt.Errorf("no agent is configured in Settings > Providers")
	}
	return a.CreateChat(Chat{
		WorkspaceID:    p.WorkspaceID,
		Title:          p.Name + " · " + now.Format("Jan 2 15:04"),
		AgentKind:      inst.ID,
		Transport:      chatTransportOf(*inst),
		ProfileID:      inst.ID,
		LastActivityAt: now.UnixMilli(),
	})
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
// returns the delivered ids — one source of the delivery sweep, run alone.
func (a *App) fireDueSchedules(now time.Time, send func(chatID int64, text string) error) []int64 {
	if a.db == nil {
		return nil
	}
	var fired []int64
	a.runDeliveries(a.scheduleDeliveries(now, func(c int64, t string, _ []string) error { return send(c, t) }, &fired))
	return fired
}

// scheduleDeliveries offers every unpaused item due at `now`.
func (a *App) scheduleDeliveries(now time.Time, send chatSender, fired *[]int64) []pendingDelivery {
	rows, err := a.db.Query(`SELECT `+scheduleCols+` FROM chat_schedules WHERE due_at <= ? AND paused = 0 ORDER BY due_at, id`, now.UnixMilli())
	if err != nil {
		return nil
	}
	var due []control.ScheduledPrompt
	for rows.Next() {
		if p, err := scanSchedule(rows); err == nil {
			due = append(due, p)
		}
	}
	rows.Close()

	var out []pendingDelivery
	for _, it := range due {
		it := it
		// A new-chat run has no chat to share a pass with: key it by its own id.
		key := it.ChatID
		if it.NewChat {
			key = -it.ID
		}
		// done: a one-shot is gone, a recurring item moves to its next slot
		// from now (missed slots are not caught up).
		done := func(errText string) {
			next, ok := control.NextDue(it.Rule, now, time.Local)
			if !ok {
				_, _ = a.db.Exec(`DELETE FROM chat_schedules WHERE id = ?`, it.ID)
				return
			}
			_, _ = a.db.Exec(`UPDATE chat_schedules SET due_at = ?, last_run_at = ?, attempts = 0, last_error = ? WHERE id = ?`,
				next.UnixMilli(), now.UnixMilli(), errText, it.ID)
		}
		out = append(out, pendingDelivery{ChatID: key, Source: deliverSchedule, Pos: it.DueAt.UnixMilli(), CreatedAt: it.ID, deliver: func() bool {
			var err error
			if it.NewChat {
				var c Chat
				if c, err = a.startScheduledChat(it, now); err == nil {
					if err = send(c.ID, it.Text, nil); err != nil {
						_ = a.DeleteChat(c.ID) // a row whose process never started
					}
				}
			} else {
				if _, err := a.chatByID(it.ChatID); errors.Is(err, sql.ErrNoRows) {
					_, _ = a.db.Exec(`DELETE FROM chat_schedules WHERE chat_id = ?`, it.ChatID) // chat deleted
					return false
				}
				if !a.chatSettled(strconv.FormatInt(it.ChatID, 10)) {
					return false // a turn is open: wait for it to settle
				}
				err = send(it.ChatID, it.Text, nil)
			}
			if err != nil {
				if errors.Is(err, errChatBusy) {
					return false
				}
				if it.Attempts+1 >= chatScheduleMaxAttempts {
					log.Printf("schedule %d: dropped after %d attempts: %v", it.ID, it.Attempts+1, err)
					done(err.Error())
					return true
				}
				_, _ = a.db.Exec(`UPDATE chat_schedules SET attempts = attempts + 1, last_error = ?, due_at = ? WHERE id = ?`,
					err.Error(), now.Add(chatScheduleRetryDelay).UnixMilli(), it.ID)
				return true
			}
			done("")
			if fired != nil {
				*fired = append(*fired, it.ID)
			}
			return true
		}})
	}
	return out
}

func (a *App) chatScheduleLoop() {
	for range time.Tick(chatScheduleSweepInterval) {
		a.deliverPending(time.Now(), a.chatSendSettled) // notices, queue, schedules (chatdelivery.go)
		a.wakeSnoozedChats(time.Now())
	}
}
