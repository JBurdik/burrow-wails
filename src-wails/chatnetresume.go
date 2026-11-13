package main

import (
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// Waiting for connection (threadbear): a turn that failed because the
// internet went, or was cut off mid-turn because Burrow / the Mac restarted,
// carries on by itself once the network is back — the user doesn't have to
// type "continue". chats.net_wait_at (read-only on the wire) marks the
// episode; the delivery pass sends one continue prompt once the chat has
// settled and a probe reaches the network, and any turn that starts clears it.
// Same shape as the usage-limit resume (chatlimit.go).

const netResumeText = "Continue where you left off — the connection dropped (or Burrow restarted) in the middle of your last turn."
const netResumeMaxTries = 5

func chatNetResumeAlters() []string {
	return []string{
		`ALTER TABLE chats ADD COLUMN net_wait_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chats ADD COLUMN net_wait_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chats ADD COLUMN net_resume_tries INTEGER NOT NULL DEFAULT 0`,
	}
}

var netFailureRe = regexp.MustCompile(`(?i)connection (error|reset|refused|closed)|econnreset|econnrefused|enotfound|eai_again|etimedout|enetunreach|ehostunreach|socket hang up|fetch failed|network (error|is unreachable)|stream disconnected|error sending request|getaddrinfo|unable to connect|request timed out|no internet|offline`)

// isNetworkFailure: a turn error that is the network's fault, not the
// model's or the request's (a usage limit is chatlimit.go's).
func isNetworkFailure(msg string) bool {
	return msg != "" && netFailureRe.MatchString(msg)
}

// noteNetFailure records a network-failed turn (from emitChatLine's events).
func (a *App) noteNetFailure(chatID string, ev ProviderRuntimeEvent, now time.Time) {
	if a.db == nil || ev.Type != EvtTurnFailed || ev.UsageLimit || !isNetworkFailure(ev.Message) {
		return
	}
	a.setNetWait(chatIDOf(chatID), "network", now)
}

func (a *App) setNetWait(chatID int64, reason string, now time.Time) {
	res, err := a.db.Exec(`UPDATE chats SET net_wait_at = ?, net_wait_reason = ? WHERE id = ? AND archived_at = 0`, now.UnixMilli(), reason, chatID)
	if err != nil {
		log.Printf("chat %d: record net wait: %v", chatID, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		busEmit("chats-changed", nil)
	}
}

// clearNetWaitOnRunning: a turn that runs ends the episode (ours or the user's).
func (a *App) clearNetWaitOnRunning(chatID string) {
	res, err := a.db.Exec(`UPDATE chats SET net_wait_at = 0, net_wait_reason = '', net_resume_tries = 0 WHERE id = ? AND net_wait_at != 0`, chatIDOf(chatID))
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			busEmit("chats-changed", nil)
		}
	}
}

// ChatNetDismiss is the banner's Dismiss: don't carry on by itself.
func (a *App) ChatNetDismiss(chatID int64) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if _, err := a.db.Exec(`UPDATE chats SET net_wait_at = 0, net_wait_reason = '', net_resume_tries = 0 WHERE id = ?`, chatID); err != nil {
		return err
	}
	busEmit("chats-changed", nil)
	return nil
}

// markRestartCutTurns (startup): a chat whose phase was still running when
// the core stopped lost that turn with its process. Its phase is settled (an
// interrupt, as if stopped) and it waits to carry on like a network drop.
func (a *App) markRestartCutTurns() {
	if a.phases == nil || a.db == nil {
		return
	}
	now := time.Now()
	for key, ph := range a.phases.All() {
		id, ok := strings.CutPrefix(key, "chat:")
		if !ok || ph.State != agentphase.Running {
			continue
		}
		a.applyChatPhase(id, agentphase.Event{Kind: agentphase.Interrupt})
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			a.setNetWait(n, "restart", now)
		}
	}
}

// netReachable probes the network (a TCP connect to the model providers),
// cached briefly so a pass with many waiting chats probes once. A var for tests.
var netReachable = func() bool {
	netProbe.mu.Lock()
	defer netProbe.mu.Unlock()
	if time.Since(netProbe.at) < 10*time.Second {
		return netProbe.ok
	}
	netProbe.ok = false
	for _, addr := range []string{"api.anthropic.com:443", "chatgpt.com:443", "1.1.1.1:443"} {
		if c, err := net.DialTimeout("tcp", addr, 3*time.Second); err == nil {
			_ = c.Close()
			netProbe.ok = true
			break
		}
	}
	netProbe.at = time.Now()
	return netProbe.ok
}

var netProbe struct {
	mu sync.Mutex
	at time.Time
	ok bool
}

// netResumeDeliveries offers a continue prompt for every waiting chat.
func (a *App) netResumeDeliveries(now time.Time, send chatSender, resumed *[]int64) []pendingDelivery {
	type row struct{ id, waitAt, tries int64 }
	rows, err := a.db.Query(`SELECT id, net_wait_at, net_resume_tries FROM chats WHERE net_wait_at != 0 AND archived_at = 0`)
	if err != nil {
		return nil
	}
	var due []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.waitAt, &r.tries) == nil {
			due = append(due, r)
		}
	}
	rows.Close()
	var out []pendingDelivery
	for _, r := range due {
		r := r
		out = append(out, pendingDelivery{ChatID: r.id, Source: deliverNetResume, Pos: r.waitAt, deliver: func() bool {
			if !a.chatSettled(strconv.FormatInt(r.id, 10)) || !netReachable() {
				return false // wait: a turn is open, or still offline
			}
			if r.tries >= netResumeMaxTries {
				_ = a.ChatNetDismiss(r.id) // it kept failing: leave it to the user
				return false
			}
			// Claim: the episode stays marked (a failed send keeps waiting) but
			// counts the try; the turn it starts clears it.
			res, err := a.db.Exec(`UPDATE chats SET net_resume_tries = net_resume_tries + 1 WHERE id = ? AND net_wait_at = ?`, r.id, r.waitAt)
			if err != nil {
				return false
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return false
			}
			if err := send(r.id, netResumeText, nil); err != nil {
				log.Printf("chat %d: network resume not sent: %v", r.id, err)
				return false
			}
			if resumed != nil {
				*resumed = append(*resumed, r.id)
			}
			return true
		}})
	}
	return out
}
