package main

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"
)

// Versioned splices, streaming tail appends and chat_subscribe — ADR 0001,
// "Wire shape (phase B)".
//
// Every splice is {baseRev, rev, fromIndex, messages}: "from fromIndex on the
// transcript is these rows, and this applies on top of baseRev". rev is the
// chat's folded_ord (the stream position folded up to), so it grows for the
// life of the chat — chat_clear keeps counting. A client applies a splice only
// when baseRev == its rev, or when it is `authoritative` (rewind, clear, a
// chat_subscribe ring miss); anything else means it missed one and catches up
// with chat_subscribe(sinceRev).
//
// While a row streams, the pure text growth goes out as transient
// `chat-tail-<id>` appends, coalesced to one per chatTailFlushAfter per chat,
// never rev'd, never ringed or persisted. The next rev'd splice replaces what
// they built (its fromIndex reaches back over every row they touched, so a
// dropped tail heals on the next structural change).

const (
	chatTailPrefix = "chat-tail-"
	// chatSpliceRing is how many recent splices per chat chat_subscribe can
	// replay; chatSpliceRingBytes caps their encoded size, so a burst of huge
	// tool outputs can't pin unbounded memory. The newest splice always stays.
	chatSpliceRing      = 64
	chatSpliceRingBytes = 2 << 20
	// chatSubscribePage is the window an authoritative subscribe splice sends.
	chatSubscribePage = 60
)

// chatTailFlushAfter is a var so tests can shorten it.
var chatTailFlushAfter = 100 * time.Millisecond

// ChatSplice is the chat-messages-changed-<id> payload.
type ChatSplice struct {
	BaseRev       int64         `json:"baseRev"`
	Rev           int64         `json:"rev"`
	FromIndex     int           `json:"fromIndex"`
	Messages      []ChatMessage `json:"messages"`
	Authoritative bool          `json:"authoritative,omitempty"`
}

// ChatMessagesChanged is the pre-phase-B name of the same payload.
type ChatMessagesChanged = ChatSplice

// ChatTail is the chat-tail-<id> payload: append Append to the text of the
// row at Index, which is Offset UTF-16 code units long before it (so a client
// that already has the text does row.text = row.text.slice(0, offset) + append
// and a repeat is harmless). Applies only on top of Rev.
type ChatTail struct {
	Rev    int64  `json:"rev"`
	Index  int    `json:"index"`
	Offset int    `json:"offset"`
	Append string `json:"append"`
	Kind   string `json:"kind"` // text | thinking
}

// ChatInProgress is the streaming row's text so far (chat_subscribe).
type ChatInProgress struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// ChatSubscribeReply is chat_subscribe's reply.
type ChatSubscribeReply struct {
	Rev        int64           `json:"rev"`
	Splices    []ChatSplice    `json:"splices"`
	InProgress *ChatInProgress `json:"inProgress"`
}

// chatSpliceLog is one chat's emitted-splice history. It lives on the stream
// writer, not on the chatTail, because a tail is dropped and re-adopted (a
// client save, a rewind) while clients keep holding the rev it emitted.
//
// Lock order: chatTail.mu → chatSpliceLog.mu. The tail flush timer takes only
// this mu. busEmit runs under it so splices and tails leave in order.
type chatSpliceLog struct {
	mu       sync.Mutex
	rev      int64 // rev of the last splice emitted
	ring     []ChatSplice
	sizes    []int // encoded size of each ring entry
	bytes    int   // sum of sizes
	tailFrom int   // lowest row index a tail touched since the last splice, -1 none
	pend     *ChatTail
	timer    *time.Timer
}

// spliceLog returns the chat's log, creating it at rev `init` (the stored
// folded_ord: what a page read from the table reports) when absent.
func (w *chatStreamWriter) spliceLog(chatID string, init int64) *chatSpliceLog {
	w.mu.Lock()
	defer w.mu.Unlock()
	lg := w.splices[chatID]
	if lg == nil {
		lg = &chatSpliceLog{rev: init, tailFrom: -1}
		w.splices[chatID] = lg
	}
	return lg
}

func (w *chatStreamWriter) peekSpliceLog(chatID string) *chatSpliceLog {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.splices[chatID]
}

// dropSpliceLog forgets a deleted chat's log. Called with w.mu held.
func (w *chatStreamWriter) dropSpliceLogLocked(chatID string) {
	if lg := w.splices[chatID]; lg != nil {
		lg.mu.Lock()
		if lg.timer != nil {
			lg.timer.Stop()
		}
		lg.pend = nil
		lg.mu.Unlock()
		delete(w.splices, chatID)
	}
}

// chatRev is the rev a reader of the stored transcript reports: the log's,
// else the stored folded_ord.
func (a *App) chatRev(chatID string) int64 {
	w := a.chatStream()
	if w == nil {
		return 0
	}
	if lg := w.peekSpliceLog(chatID); lg != nil {
		lg.mu.Lock()
		defer lg.mu.Unlock()
		return lg.rev
	}
	ord, _, _ := w.foldedOrd(chatID)
	return ord
}

// emitSplice publishes rows [from, len) of t as the next splice. Called
// with t.mu held.
func (a *App) emitSplice(w *chatStreamWriter, chatID string, t *chatTail, from int, authoritative bool) {
	lg := w.spliceLog(chatID, t.persistedOrd)
	lg.mu.Lock()
	defer lg.mu.Unlock()
	lg.flushTailLocked(chatID)
	if lg.tailFrom >= 0 && lg.tailFrom < from {
		from = lg.tailFrom
	}
	from = min(max(from, 0), t.length())
	// rev = folded_ord, kept strictly increasing: two goroutines emitting
	// lines for one chat can fold ord N after N+1, which leaves folded_ord
	// where it was.
	rev := max(t.foldedOrd, lg.rev+1)
	rows := t.slice(from)
	for i := range rows {
		rows[i] = stubRowImages(rows[i], from+i, false)
	}
	sp := ChatSplice{BaseRev: lg.rev, Rev: rev, FromIndex: from, Messages: rows, Authoritative: authoritative}
	lg.push(sp)
	lg.rev = rev
	lg.tailFrom = -1
	busEmit(chatMessagesChangedPrefix+chatID, sp)
}

// push adds sp to the ring, dropping the oldest past the count or byte cap.
func (lg *chatSpliceLog) push(sp ChatSplice) {
	b, _ := json.Marshal(sp)
	lg.ring = append(lg.ring, sp)
	lg.sizes = append(lg.sizes, len(b))
	lg.bytes += len(b)
	drop := 0
	for len(lg.ring)-drop > 1 && (len(lg.ring)-drop > chatSpliceRing || lg.bytes > chatSpliceRingBytes) {
		lg.bytes -= lg.sizes[drop]
		drop++
	}
	if drop > 0 {
		// Copy, so the dropped splices' rows can be collected.
		lg.ring = append([]ChatSplice(nil), lg.ring[drop:]...)
		lg.sizes = append([]int(nil), lg.sizes[drop:]...)
	}
}

// addTail queues text appended to the streaming row at index (whose text was
// `before`), flushing on its own after chatTailFlushAfter.
func (lg *chatSpliceLog) addTail(chatID string, index int, kind, before, text string) {
	lg.mu.Lock()
	defer lg.mu.Unlock()
	if p := lg.pend; p != nil && (p.Index != index || p.Kind != kind || p.Rev != lg.rev) {
		lg.flushTailLocked(chatID)
	}
	if lg.pend == nil {
		lg.pend = &ChatTail{Rev: lg.rev, Index: index, Offset: utf16Len(before), Kind: kind}
		lg.timer = time.AfterFunc(chatTailFlushAfter, func() {
			lg.mu.Lock()
			defer lg.mu.Unlock()
			lg.flushTailLocked(chatID)
		})
	}
	lg.pend.Append += text
	if lg.tailFrom < 0 || index < lg.tailFrom {
		lg.tailFrom = index
	}
}

func (lg *chatSpliceLog) flushTailLocked(chatID string) {
	if lg.pend == nil {
		return
	}
	if lg.timer != nil {
		lg.timer.Stop()
		lg.timer = nil
	}
	p := *lg.pend
	lg.pend = nil
	busEmit(chatTailPrefix+chatID, p)
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// emitAuthoritativeSplice publishes the transcript from `from` on as an
// authoritative splice — what rewind (from = the cut) and clear (from = 0)
// send after they replace the transcript.
func (a *App) emitAuthoritativeSplice(chatID string, from int) {
	w := a.chatStream()
	if w == nil {
		return
	}
	t := a.lockFoldTail(w, chatID, w.peekNextOrd(chatID))
	defer t.mu.Unlock()
	a.emitSplice(w, chatID, t, from, true)
}

// ChatSubscribe catches a client up from sinceRev: the splices since then
// while the ring still has them, else one authoritative splice of the newest
// window — plus the streaming row's text so far (tail appends are not in the
// ring).
func (a *App) ChatSubscribe(chatID int, sinceRev int64) (ChatSubscribeReply, error) {
	id := strconv.Itoa(chatID)
	reply := ChatSubscribeReply{Splices: []ChatSplice{}}
	w := a.chatStream()
	if w == nil {
		return reply, nil
	}
	w.mu.Lock()
	t := w.tails[id]
	w.mu.Unlock()
	if t != nil {
		t.mu.Lock()
		if t.loaded && !t.noPersist {
			defer t.mu.Unlock()
			lg := w.spliceLog(id, t.persistedOrd)
			lg.mu.Lock()
			defer lg.mu.Unlock()
			reply.Rev = lg.rev
			if n := t.length(); n > 0 {
				if last := t.at(n - 1); last.Partial {
					reply.InProgress = &ChatInProgress{Index: n - 1, Text: last.Text}
				}
			}
			if sinceRev == lg.rev {
				return reply, nil
			}
			for i, sp := range lg.ring {
				if sp.BaseRev == sinceRev {
					reply.Splices = append(reply.Splices, lg.ring[i:]...)
					return reply, nil
				}
			}
			start := max(0, t.length()-chatSubscribePage)
			rows := t.slice(start)
			for i := range rows {
				rows[i] = stubRowImages(rows[i], start+i, false)
			}
			reply.Splices = append(reply.Splices, ChatSplice{BaseRev: sinceRev, Rev: lg.rev, FromIndex: start, Messages: rows, Authoritative: true})
			return reply, nil
		}
		t.mu.Unlock()
	}
	// Not folded in this process: the table is the transcript.
	reply.Rev = a.chatRev(id)
	if sinceRev == reply.Rev {
		return reply, nil
	}
	page, err := a.chatPageRows(chatID, -1, chatSubscribePage)
	if err != nil {
		return reply, err
	}
	reply.Splices = append(reply.Splices, ChatSplice{BaseRev: sinceRev, Rev: reply.Rev, FromIndex: page.start, Messages: page.rows, Authoritative: true})
	return reply, nil
}

type chatPage struct {
	rows  []ChatMessage
	start int
}

// chatPageRows is the newest-`limit` window ending before `before`, decoded
// and image-stubbed.
func (a *App) chatPageRows(chatID, before, limit int) (chatPage, error) {
	p, err := a.LoadChatMessagesPage(chatID, before, limit)
	if err != nil {
		return chatPage{}, err
	}
	rows := make([]ChatMessage, 0, len(p.Messages))
	for _, raw := range p.Messages {
		var m ChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return chatPage{}, err
		}
		rows = append(rows, m)
	}
	return chatPage{rows: rows, start: p.Start}, nil
}
