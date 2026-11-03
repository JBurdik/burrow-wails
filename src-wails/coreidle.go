package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"
)

// The external core (plan 009) quits on its own once nobody needs it: no UI
// connected for coreIdleGrace AND no work that would be lost or delayed by
// stopping. A core that has work stays even with every window closed — that
// is the point of splitting it out.

const (
	coreIdleGrace = 5 * time.Minute
	coreIdleTick  = 30 * time.Second
)

// coreQuit stops the core and every UI with it; nil outside the core role.
var coreQuit func()

// ensureUIOpen starts a UI when none is connected and waits for it (core
// role only): a UI-performed control verb (new_tab, workspace_create, spawn
// into a tab) from `burrow` or the phone has nobody to perform it otherwise.
var ensureUIOpen func()

// coreWork is everything that keeps a core alive with no UI attached.
type coreWork struct {
	turnOpen     bool // a chat is running or waiting on the user
	queued       bool // chat_queue, undelivered sub-agent notices, armed limit resume
	scheduled    bool // an unpaused schedule
	remoteAccess bool // the phone reaches this core
	loginItem    bool // the user asked for an always-on core
}

func (w coreWork) any() bool {
	return w.turnOpen || w.queued || w.scheduled || w.remoteAccess || w.loginItem
}

// coreShouldExit is the pure verdict: idle long enough, nothing to do.
func coreShouldExit(uiConnected bool, lastUI, now time.Time, w coreWork) bool {
	return !uiConnected && now.Sub(lastUI) >= coreIdleGrace && !w.any()
}

// coreIdleTiming: BURROW_CORE_IDLE_GRACE (a Go duration) shortens the wait
// for testing; the tick follows it.
func coreIdleTiming() (grace, tick time.Duration) {
	grace, tick = coreIdleGrace, coreIdleTick
	if d, err := time.ParseDuration(os.Getenv("BURROW_CORE_IDLE_GRACE")); err == nil && d > 0 {
		grace, tick = d, d/4
	}
	return grace, tick
}

func (a *App) coreWork() coreWork {
	var w coreWork
	if a.phases != nil {
		for key, ph := range a.phases.All() {
			if strings.HasPrefix(key, "chat:") && ph.InFlight() {
				w.turnOpen = true
				break
			}
		}
	}
	if a.db != nil {
		exists := func(q string) bool {
			var one int
			return a.db.QueryRow(q).Scan(&one) == nil
		}
		w.queued = exists(`SELECT 1 FROM chat_queue LIMIT 1`) ||
			exists(`SELECT 1 FROM subagent_notices WHERE delivered_at = 0 LIMIT 1`) ||
			exists(`SELECT 1 FROM chats WHERE limit_resume_at > 0 LIMIT 1`)
		w.scheduled = exists(`SELECT 1 FROM chat_schedules WHERE paused = 0 LIMIT 1`)
	}
	if p, err := httpEnabledPrefPath(); err == nil {
		_, err := os.Stat(p)
		w.remoteAccess = err == nil
	}
	w.loginItem = a.coreAtLogin()
	return w
}

// coreAtLogin reads uiPrefs.coreAtLogin fresh (Settings → General). An
// unreadable config counts as on: never quit a core on a guess.
func (a *App) coreAtLogin() bool {
	raw, err := a.ReadConfig()
	if err != nil {
		return true
	}
	var cfg struct {
		UI struct {
			On bool `json:"coreAtLogin"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return true
	}
	return cfg.UI.On
}

// runCoreIdleExit calls quit once the core should exit. Starts counting from
// now, so a core a CLI verb just started gets the full grace too.
func (a *App) runCoreIdleExit(quit func()) {
	grace, tick := coreIdleTiming()
	lastUI := time.Now()
	for range time.Tick(tick) {
		now := time.Now()
		ui := a.remoteWS != nil && a.remoteWS.uiConnected()
		if ui {
			lastUI = now
			continue
		}
		// Shift by the configured grace so the pure verdict keeps one constant.
		if coreShouldExit(ui, lastUI.Add(grace-coreIdleGrace), now, a.coreWork()) {
			log.Printf("core: idle with no UI since %s, exiting", lastUI.Format(time.TimeOnly))
			quit()
			return
		}
	}
}
