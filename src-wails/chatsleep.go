package main

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	"burrow/internal/agentphase"
)

// Auto-sleep: an agent CLI holds ~150 MB doing nothing, so a settled chat
// nobody has touched for N minutes (Settings → General, uiPrefs
// .chatAutoSleepMinutes, default 60, 0 = off) gets its process stopped and its
// row stamped `slept_at`. Nothing else changes: the transcript is on disk and
// the next ChatSend starts the process again with --resume (ensureClaude /
// ensureRPC), which also clears the stamp. Opening the chat does not wake it.
//
// It replaces agentproc.ReapIdle, which reaped on stdout silence alone — a CLI
// blocked on a permission prompt is silent too, and got killed mid-question.

const (
	defaultChatAutoSleepMinutes = 60
	chatSleepSweepInterval      = time.Minute
)

// chatSleepEligible is the whole rule. Only a settled phase sleeps: running
// and waiting_* (a pending permission or question) never do, and a queued
// message only exists while a turn runs. busy covers a turn the transport
// still has open when the phase disagrees (an ACP prompt without its
// response). "Open on screen and focused" arrives as activity: the watching
// client pings ChatMarkActive, so a watched chat never goes stale.
func chatSleepEligible(state agentphase.State, busy bool, lastActive, now time.Time, idle time.Duration) bool {
	if idle <= 0 || busy {
		return false
	}
	switch state {
	case agentphase.Idle, agentphase.Done, agentphase.Failed, agentphase.Stale:
		return now.Sub(lastActive) >= idle
	}
	return false
}

// touchChat records activity on a chat process: a send, a start, any line it
// streams, a watching client's ping. It also registers the id with the sweep.
func (r *chatRuntime) touchChat(id string, now time.Time) {
	r.active.Store(id, now)
}

// ChatMarkActive is the watching client's heartbeat (chatAttention pings every
// minute for each chat that is on screen while the window has focus).
func (a *App) ChatMarkActive(chatID int64) {
	a.chatRT.touchChat(strconv.FormatInt(chatID, 10), time.Now())
}

// chatAutoSleepIdle reads the setting fresh each sweep. Absent = the default,
// an unreadable config = off (never kill on a guess).
func (a *App) chatAutoSleepIdle() time.Duration {
	raw, err := a.ReadConfig()
	if err != nil {
		return 0
	}
	var cfg struct {
		UI struct {
			Minutes *int `json:"chatAutoSleepMinutes"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return 0
	}
	if cfg.UI.Minutes == nil {
		return defaultChatAutoSleepMinutes * time.Minute
	}
	return time.Duration(*cfg.UI.Minutes) * time.Minute
}

// sleepIdleChats stops every live chat process chatSleepEligible allows at
// `now` and returns the chat ids it put to sleep.
func (a *App) sleepIdleChats(now time.Time, idle time.Duration) []string {
	var ids, slept []string
	a.chatRT.active.Range(func(k, _ any) bool {
		ids = append(ids, k.(string))
		return true
	})
	for _, id := range ids {
		if a.sleepChat(id, now, idle, false) {
			slept = append(slept, id)
		}
	}
	return slept
}

// ChatSleep puts one chat to sleep right now (manual settle). Same guards as
// the sweep — settled phase, no open turn — minus the idle window; otherwise a
// silent no-op.
func (a *App) ChatSleep(chatID int64) bool {
	return a.sleepChat(strconv.FormatInt(chatID, 10), time.Now(), 0, true)
}

// sleepChat decides and stops under the chat lock, so a send can't land
// between the check and the kill. manual skips the idle-window test (and the
// need for a recorded activity).
func (a *App) sleepChat(id string, now time.Time, idle time.Duration, manual bool) bool {
	defer a.chatRT.lock(id)()
	var last any = time.Time{}
	if manual {
		idle = time.Nanosecond
	} else {
		var ok bool
		if last, ok = a.chatRT.active.Load(id); !ok {
			return false
		}
	}
	claudeLive := a.claudeMgr().Alive(id)
	sess, rpcLive := a.acpReg().get(id)
	if !claudeLive && !rpcLive {
		a.chatRT.active.Delete(id) // gone (stopped, exited); the next start re-registers it
		return false
	}
	busy := false
	if rpcLive {
		sess.mu.Lock()
		busy = sess.pendingTurn != 0
		sess.mu.Unlock()
	}
	var state agentphase.State
	if a.phases != nil {
		state = a.phases.Get("chat:" + id).State
	}
	if !chatSleepEligible(state, busy, last.(time.Time), now, idle) {
		return false
	}
	a.stopProcess(id) // the phase is settled, so its Interrupt is a no-op
	a.chatRT.active.Delete(id)
	if a.db != nil {
		if _, err := a.db.Exec(`UPDATE chats SET slept_at = ? WHERE id = ?`, now.UnixMilli(), chatIDOf(id)); err == nil {
			busEmit("chats-changed", nil)
		}
	}
	return true
}

// markChatAwake clears slept_at once a process is running again.
func (a *App) markChatAwake(chatID int64) {
	if a.db == nil {
		return
	}
	if res, err := a.db.Exec(`UPDATE chats SET slept_at = 0 WHERE id = ? AND slept_at != 0`, chatID); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			busEmit("chats-changed", nil)
		}
	}
}

func (a *App) chatSleepLoop() {
	for range time.Tick(chatSleepSweepInterval) {
		if slept := a.sleepIdleChats(time.Now(), a.chatAutoSleepIdle()); len(slept) > 0 {
			log.Printf("auto-sleep: stopped %d idle chat process(es): %v", len(slept), slept)
		}
	}
}
