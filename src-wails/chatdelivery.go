package main

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Everything Go sends into a chat on its own — sub-agent notices, the user's
// queued follow-ups, scheduled prompts — goes out through one sweep, only once
// the chat has settled (ChatSend into a running turn is a steer, not a queued
// prompt), at most one item per chat per pass: that item's turn makes the
// next one wait its turn. deliveryOrder decides which goes first.

type deliverySource int

const (
	deliverNotice      deliverySource = iota // a sub-agent finished / blocked: the thread acts on it first
	deliverLimitResume                       // "Continue where you left off." after a usage limit (chatlimit.go)
	deliverQueue                             // the user's queued follow-ups, head first
	deliverSchedule                          // scheduled prompts / wake-ups, by due time
)

// chatSender is ChatSend's shape; production wires chatSendSettled.
type chatSender func(chatID int64, text string, images []string) error

type pendingDelivery struct {
	ChatID    int64
	Source    deliverySource
	Pos       int64 // within a source: queue ord, schedule due time
	CreatedAt int64
	// deliver claims and sends; false = nothing went out (dropped, chat busy),
	// so the next item for the chat may go this pass.
	deliver func() bool
}

// deliveryOrder is the order a settled chat receives pending items in (t3code's
// queuedRunsInDeliveryOrder): automatic completions first, then an armed
// usage-limit resume, then the user's queue by position, then schedules; ties
// by creation.
func deliveryOrder(items []pendingDelivery) []pendingDelivery {
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(x, y pendingDelivery) int {
		return cmp.Or(cmp.Compare(x.Source, y.Source), cmp.Compare(x.Pos, y.Pos), cmp.Compare(x.CreatedAt, y.CreatedAt))
	})
	return out
}

// deliveryMu: the sweep and a settle kick must never run a claim → send →
// mark sequence for the same item concurrently.
var deliveryMu sync.Mutex

func (a *App) runDeliveries(items []pendingDelivery) {
	deliveryMu.Lock()
	defer deliveryMu.Unlock()
	sent := map[int64]bool{}
	for _, d := range deliveryOrder(items) {
		if sent[d.ChatID] {
			continue
		}
		if d.deliver() {
			sent[d.ChatID] = true
		}
	}
}

// deliverPending runs one pass over every source.
func (a *App) deliverPending(now time.Time, send chatSender) {
	if a.db == nil {
		return
	}
	var items []pendingDelivery
	items = append(items, a.noticeDeliveries(now, send, nil)...)
	items = append(items, a.limitResumeDeliveries(now, send, nil)...)
	items = append(items, a.queueDeliveries(send, nil)...)
	items = append(items, a.scheduleDeliveries(now, send, nil)...)
	// A chat still on a usage limit gets nothing: each send would fail on it.
	limited := a.limitedChats(now)
	items = slices.DeleteFunc(items, func(d pendingDelivery) bool { return limited[d.ChatID] })
	a.runDeliveries(items)
}

var errChatBusy = errors.New("chat has a turn in flight")

// chatSendSettled is ChatSend that re-checks "settled" under the chat lock: a
// user's own ChatSend may be mid-start (phase still idle while the process
// spawns), and a delivery racing it must not become a steer.
func (a *App) chatSendSettled(chatID int64, text string, images []string) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if !a.chatSettled(id) {
		return errChatBusy
	}
	return a.chatSendLocked(c, id, text, images)
}

// kickChatDelivery runs a pass shortly after a turn settles (or an item is
// queued into a settled chat), so a follow-up doesn't wait for the 15 s sweep.
// Delayed like the client's old nextTick drain: the finishing turn's last
// lines and the transport's own turn bookkeeping land first.
func (a *App) kickChatDelivery() {
	if a.ctx == nil {
		return // tests drive deliverPending directly
	}
	time.AfterFunc(300*time.Millisecond, func() { a.deliverPending(time.Now(), a.chatSendSettled) })
}
