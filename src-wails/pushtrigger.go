package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
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

// nativeNotifier posts an OS notification; set only by the external core
// (plan 009) once macOS authorized it — nil in-process. It is the fallback
// for a tray toast (trayToast) that could not be shown.
var nativeNotifier atomic.Pointer[func(title, body, url, tag string)]

// trayToast is the "you are away" notice dropped from the tray icon
// (desktop.go's traytoast window, macOS) instead of an OS banner. Route is a
// main-window route ("/ws/3?chat=7"), opened by TrayOpen on click.
type trayToast struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Project string `json:"project"`
	Route   string `json:"route"`
	Reason  string `json:"reason"`
	Parent  string `json:"parent,omitempty"` // a sub-agent's parent thread title
}

// awayNotifier: something in this process tells a desktop user who is away.
func awayNotifier() bool { return nativeNotifier.Load() != nil || trayToastReady() }

// pushPhaseChanged runs under PhaseStore.emitMu: decide cheaply, send async.
func (a *App) pushPhaseChanged(key string, prev, next agentphase.Phase) {
	if a.push == nil {
		return
	}
	if reason := pushReason(key, prev.State, next.State); reason != "" {
		go a.pushFor(key, reason, next, true)
	} else if awayNotifier() && strings.HasPrefix(key, "pty:") && next.State == agentphase.Done && prev.State != next.State {
		// A tab's finished turn: the desktop said so (Terminal.vue did), the
		// phone deliberately does not (pushReason).
		go a.pushFor(key, "done", next, false)
	}
}

// claimNative is claim with its own rate memory (a native banner and a phone
// push must not use up each other's slot) and a focus test: a focused main
// window toasts in-app instead. focusKnown (in-process: mainWindowFocused)
// decides on its own; otherwise (external core, the window is another
// process) ANY chat watched lately stands in for a focused window.
// ponytail: there a terminal-only view sends no chat_mark_active, so it still
// gets banners; a window-focus signal from the UI process would fix that.
func (p *webPusher) claimNative(key string, now time.Time, focused, focusKnown bool) bool {
	if focusKnown && focused {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var watched time.Time
	if !focusKnown {
		for _, t := range p.watched {
			if t.After(watched) {
				watched = t
			}
		}
	}
	if !pushGate(watched, p.lastNative[key], now) {
		return false
	}
	p.lastNative[key] = now
	return true
}

// pushFor builds the notification for one phase key and sends it — natively
// (external core) and, if phone, as Web Push — where each gate lets it.
func (a *App) pushFor(key, reason string, ph agentphase.Phase, phone bool) {
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
	var ws, parent int64
	var parentTitle string
	switch kind {
	case "chat":
		var archived int64
		err := a.db.QueryRow(`SELECT title, workspace_id, parent_chat_id, archived_at FROM chats WHERE id = ?`, id).
			Scan(&msg.Title, &ws, &parent, &archived)
		if err != nil || archived != 0 {
			return
		}
		msg.URL = fmt.Sprintf("#/ws/%d?chat=%s", ws, id)
		if parent != 0 {
			// A sub-agent lives under its parent thread (Right Panel), so that is what opens.
			_ = a.db.QueryRow(`SELECT title FROM chats WHERE id = ?`, parent).Scan(&parentTitle)
			msg.URL = fmt.Sprintf("#/ws/%d?chat=%d", ws, parent)
			// Its finished turn reaches the phone through the parent (sub-agent
			// notices); the desktop still gets a tray toast.
			if reason == "done" {
				phone = false
			}
		}
	case "pty":
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
		if strings.HasPrefix(ph.Detail, "Secret requested: ") {
			msg.Body = ph.Detail // the name only — a value never reaches a phase
		}
	case "done":
		msg.Body = "Done"
	case "snooze":
		msg.Body = "Snooze ended"
	}
	now := time.Now()
	focused, focusKnown := mainWindowFocused()
	if !focusKnown {
		// dev/beta: the window is another process — it reports its own focus.
		focused, focusKnown = reportedFocus(now)
	}
	if awayNotifier() && a.push.claimNative(key, now, focused, focusKnown) {
		t := trayToast{Key: key, Title: msg.Title, Body: msg.Body, Route: strings.TrimPrefix(msg.URL, "#"), Reason: reason, Parent: parentTitle}
		if parent != 0 && parentTitle == "" {
			t.Parent = "Sub-agent"
		}
		_ = a.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, ws).Scan(&t.Project)
		if !showTrayToast(t) {
			if notify := nativeNotifier.Load(); notify != nil {
				(*notify)(msg.Title, msg.Body, msg.URL, key)
			}
		}
	}
	if !phone || !a.push.claim(key, now) {
		return
	}
	payload, _ := json.Marshal(msg)
	// Topic: the push service keeps only the newest queued message per
	// chat/tab (url-safe alphabet, so "chat-12", not "chat:12").
	a.pushToAll(payload, kind+"-"+id)
}

func (a *App) pushChat(chatID int64, reason string) {
	go a.pushFor("chat:"+strconv.FormatInt(chatID, 10), reason, agentphase.Phase{}, true)
}
