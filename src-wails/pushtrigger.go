package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"burrow/internal/agentphase"
)

// When to push, decided in Go because the desktop may be closed. Hooked on
// PhaseStore.onChange (chats AND PTY agent tabs) plus the snooze wake.

const (
	// pushWatchWindow: a chat_mark_active this recent means someone is looking
	// at the chat in a focused window (chatAttention pings every 60 s).
	pushWatchWindow = 90 * time.Second
	// pushMinInterval: at most one push per chat/tab in this window.
	pushMinInterval = 30 * time.Second
)

// pushReason says what a phase transition is worth telling the phone, ""
// for nothing. A finished turn only for chats: a PTY tab's turn end is
// its own TUI's business, its prompts are not.
func pushReason(key string, prev, next agentphase.State) string {
	if prev == next {
		return ""
	}
	switch next {
	case agentphase.WaitingApproval:
		return "permission"
	case agentphase.WaitingInput:
		return "waiting"
	case agentphase.Done:
		if strings.HasPrefix(key, "chat:") {
			return "done"
		}
	}
	return ""
}

// pushGate: not while it is being watched, and not more often than
// pushMinInterval. Zero times mean "never".
func pushGate(watchedAt, lastPush, now time.Time) bool {
	if !watchedAt.IsZero() && now.Sub(watchedAt) < pushWatchWindow {
		return false
	}
	return lastPush.IsZero() || now.Sub(lastPush) >= pushMinInterval
}

func (p *webPusher) watch(key string, now time.Time) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.watched[key] = now
	p.mu.Unlock()
}

// claim applies pushGate for key and, when it passes, records the push.
func (p *webPusher) claim(key string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !pushGate(p.watched[key], p.last[key], now) {
		return false
	}
	p.last[key] = now
	return true
}

// pushPhaseChanged runs under PhaseStore.emitMu: decide cheaply, send async.
func (a *App) pushPhaseChanged(key string, prev, next agentphase.Phase) {
	if a.push == nil {
		return
	}
	if reason := pushReason(key, prev.State, next.State); reason != "" {
		go a.pushFor(key, reason, next)
	}
}

// pushFor builds the notification for one phase key and sends it if the gate
// lets it through.
func (a *App) pushFor(key, reason string, ph agentphase.Phase) {
	if a.push == nil || a.db == nil {
		return
	}
	msg := struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
		Tag   string `json:"tag"`
	}{Tag: key}
	kind, id, _ := strings.Cut(key, ":")
	switch kind {
	case "chat":
		var ws, parent, archived int64
		err := a.db.QueryRow(`SELECT title, workspace_id, parent_chat_id, archived_at FROM chats WHERE id = ?`, id).
			Scan(&msg.Title, &ws, &parent, &archived)
		// A sub-agent's finished turn goes to its parent thread (sub-agent
		// notices), not to the phone; its prompts still do.
		if err != nil || archived != 0 || (parent != 0 && reason == "done") {
			return
		}
		msg.URL = fmt.Sprintf("#/ws/%d?chat=%s", ws, id)
	case "pty":
		var ws int64
		var title sql.NullString
		err := a.db.QueryRow(`SELECT workspace_id, COALESCE(NULLIF(title, ''), default_title) FROM terminal_tabs
			WHERE pty_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&ws, &title)
		if err != nil {
			return
		}
		msg.Title = title.String
		msg.URL = fmt.Sprintf("#/ws/%d/tab/%s", ws, id)
	default:
		return
	}
	if msg.Title == "" {
		msg.Title = ph.Title
	}
	if msg.Title == "" {
		msg.Title = "Burrow"
	}
	switch reason {
	case "permission":
		msg.Body = "Needs permission"
		if ph.Detail != "" {
			msg.Body += ": " + truncateRunes(ph.Detail, 120, "…")
		}
	case "waiting":
		msg.Body = "Waiting for you"
	case "done":
		msg.Body = "Done"
	case "snooze":
		msg.Body = "Snooze ended"
	}
	if !a.push.claim(key, time.Now()) {
		return
	}
	payload, _ := json.Marshal(msg)
	// Topic: the push service keeps only the newest queued message per
	// chat/tab (url-safe alphabet, so "chat-12", not "chat:12").
	a.pushToAll(payload, kind+"-"+id)
}

func (a *App) pushChat(chatID int64, reason string) {
	go a.pushFor("chat:"+strconv.FormatInt(chatID, 10), reason, agentphase.Phase{})
}
