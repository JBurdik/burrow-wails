package main

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The server-owned folded transcript.
//
// chatfold.go turns provider-neutral events into ChatMessage rows; this file
// is what runs that fold as lines arrive, holds the result, and writes it to
// chat_messages. The frontend used to do all three, which meant the transcript
// only existed while a client was mounted and only agreed with itself while
// exactly one client existed. chat_stream stays the single source of truth;
// chat_messages is from here on a cache of the fold — with one exception that
// is the whole reason adoption exists, below.
//
// ADOPTION, and what must never be recomputed. Existing chat_messages rows
// are, for many chats, the ONLY surviving copy of a transcript whose stream
// lines `trim` already deleted (trim is allowed to drop anything below
// folded_ord). So the first fold of a chat in this process does NOT recompute
// from chat_stream: it LOADS the stored rows as its starting state and folds
// forward from folded_ord, appending. Recomputing would silently truncate a
// long chat's history to whatever is still in the last few hundred lines.
//
// Ruling B (id continuation): foldEvents stays pure, with id = index in its
// own output slice. The offset onto adopted rows is applied HERE, by the
// caller, in chatTail.combined() — pushing a counter into foldEvents would
// make the id depend on something other than the event sequence.

const (
	// chatFoldPersistEvery is the burst cap: this many changed folds force a
	// write even inside one chatFoldPersistAfter window, so a very loud turn
	// cannot leave an unbounded number of messages unpersisted.
	chatFoldPersistEvery = 512
	// chatFoldPersistAfter is the steady-state rate limiter. A fold per token
	// driving a write is the "live-typing hang": SaveChatMessages rewrites a
	// whole transcript, and even the tail-upsert below is a transaction.
	// Coalescing on elapsed time bounds writes to roughly one per second per
	// talking chat and bounds what a crash costs to one second of stream.
	//
	// There is deliberately no background timer. The trigger is evaluated when
	// the next line arrives, so a turn that stalls mid-stream can leave its
	// last sub-second of messages unpersisted until the next line or the turn
	// boundary — which loses nothing, because folded_ord did not advance
	// either and trim therefore still keeps the stream lines those messages
	// came from. That equivalence is the point of the pair moving together.
	chatFoldPersistAfter = time.Second
)

// chatMessagesChangedPrefix is the event family clients splice from. The
// payload carries the changed TAIL, not a bare "something changed": a
// notification would make every client re-read the whole transcript per
// token, which is unshippable to a phone on a train.
const chatMessagesChangedPrefix = "chat-messages-changed-"

// ChatMessagesChanged is the changed tail of a chat's transcript. FromIndex is
// the index of Messages[0] in the whole transcript, so a client splices
// `messages[fromIndex:] = messages`.
type ChatMessagesChanged struct {
	FromIndex int           `json:"fromIndex"`
	Messages  []ChatMessage `json:"messages"`
}

// chatTail is one chat's folded transcript in memory. It is authoritative
// while the app runs — LoadChatMessages serves it rather than reading the
// table, so a client never sees a transcript older than the last coalesced
// write.
//
// Two mutexes are in play and the distinction matters:
//   - chatStreamWriter.mu guards the MAP these live in (alongside nextOrd /
//     appends) and is on the hot append path. It is never held across a SQLite
//     call.
//   - chatTail.mu serialises folding, persisting and emitting for ONE chat.
//     It IS held across this chat's own transaction, because the state must
//     not move under a write that is recording it, and across the busEmit, for
//     the same reason PhaseStore holds emitMu across its publish tail: two
//     concurrent folds that emitted after unlocking could deliver splices out
//     of order and leave a client a step behind.
type chatTail struct {
	mu sync.Mutex

	// adopted are the pre-existing stored rows, kept byte-for-byte as the
	// prefix of the transcript. Never re-derived, never rewritten.
	adopted  []ChatMessage
	idOffset int

	// st is the fold accumulator for everything folded in this process. Its
	// own ids start at 0 (foldEvents' contract); combined() adds idOffset.
	st *foldState

	// foldedOrd is the first ord NOT folded into `adopted`+`st`.
	// persistedOrd is the same number as last COMMITTED to chat_stream_state.
	// persistedOrd may lag; foldedOrd must never reach the table ahead of the
	// messages it accounts for.
	foldedOrd    int64
	persistedOrd int64

	// turnStart is the transcript index where the current turn began — the
	// conservative floor for events that touch a row behind the tail
	// (tool.completed, message.patch_user, an ACP chunk matched by id) and
	// for settleTranscript, neither of which reports which index it changed.
	turnStart int

	// dirtyFrom is the lowest transcript index changed since the last commit.
	// Rows below it are already on disk with the same payload, so a persist
	// upserts [dirtyFrom, len) and deletes beyond len — never the
	// whole-transcript DELETE+reinsert SaveChatMessages does.
	dirtyFrom int

	unsaved  int
	lastSave time.Time

	// noPersist marks a tail whose adopted prefix could not be READ. Such a
	// tail folds and publishes normally but must never write: dirtyFrom would
	// be 0 over an empty prefix, so the first persist's prune
	// (DELETE ... ord >= len) would delete the very stored transcript the read
	// failed to load. A transient SQLITE_BUSY or one corrupt payload_json is
	// enough to reach this, and losing the live fold is recoverable from
	// chat_stream — deleting the adopted prefix is not.
	noPersist bool
}

// length is the transcript's length without materializing it.
func (t *chatTail) length() int { return len(t.adopted) + len(t.st.messages) }

// at returns transcript row i: an adopted row verbatim, or this process's fold
// with the id offset applied.
func (t *chatTail) at(i int) ChatMessage {
	if i < len(t.adopted) {
		return t.adopted[i]
	}
	j := i - len(t.adopted)
	m := t.st.messages[j]
	m.ID = t.idOffset + j
	return m
}

// slice materializes the transcript from `from` onward, and only from there.
// Copying the WHOLE transcript per folded line is the same per-token cost
// Ruling D exists to keep off this path, one layer up from the DB write: a
// 2000-message adopted prefix is a quarter of a megabyte copied per token, for
// an emit that needs one message.
func (t *chatTail) slice(from int) []ChatMessage {
	from = max(from, 0)
	n := t.length()
	out := make([]ChatMessage, 0, max(n-from, 0))
	for i := from; i < n; i++ {
		out = append(out, t.at(i))
	}
	return out
}

// combined is the whole transcript. Only readers of the full transcript
// (LoadChatMessages) use it; the fold path uses slice/length.
func (t *chatTail) combined() []ChatMessage { return t.slice(0) }

// isTurnBoundary reports the events after which nothing is still streaming.
// session.exited counts because a CLI that dies mid-turn emits no
// turn.completed, and a message left partial:true would persist that way and
// render as mid-stream forever (the client's saveMessages used to filter
// partials out before saving; Go has no such filter, by design — the stored
// transcript is the fold, not a subset of it).
func isTurnBoundary(evType string) bool {
	switch evType {
	case EvtTurnCompleted, EvtTurnFailed, EvtSessionExited:
		return true
	}
	return false
}

func hasPartial(msgs []ChatMessage) bool {
	for i := range msgs {
		if msgs[i].Partial {
			return true
		}
	}
	return false
}

// foldChatLine folds one line's already-computed events into the chat's
// transcript, coalesces the write, and publishes the changed tail.
//
// It takes the events rather than the raw line on purpose: emitChatLine has
// already called NormalizeChatLine once, and normalizing twice per line would
// double the parse on the hot path for two readings that must agree anyway.
func (a *App) foldChatLine(chatID string, ord int64, events []ProviderRuntimeEvent) {
	w := a.chatStream()
	if w == nil || len(events) == 0 {
		return
	}
	t := a.chatTailFor(w, chatID, ord)

	t.mu.Lock()
	defer t.mu.Unlock()

	batchFrom := -1
	widen := func(i int) {
		if batchFrom < 0 || i < batchFrom {
			batchFrom = i
		}
	}
	settled := false

	for _, ev := range events {
		if isTurnBoundary(ev.Type) {
			if hasPartial(t.st.messages) {
				settleTranscript(t.st.messages)
				widen(t.turnStart)
			}
			t.turnStart = len(t.adopted) + len(t.st.messages)
			settled = true
			continue
		}
		before := len(t.st.messages)
		if !applyChatEvent(t.st, ev) {
			continue
		}
		switch {
		case len(t.st.messages) > before:
			// A new bubble: the only changed row is the one just appended.
			widen(len(t.adopted) + before)
		case ev.Type == EvtToolCompleted, ev.Type == EvtMessagePatchUser,
			strings.HasPrefix(ev.MessageID, "acp:"):
			// These match a row BEHIND the tail (by tool id, by "last user
			// bubble", by ACP messageId) and do not report which one, so the
			// turn's start is the honest floor.
			widen(t.turnStart)
		default:
			// A delta appended to the last message.
			widen(len(t.adopted) + before - 1)
		}
	}

	if ord >= 0 && ord+1 > t.foldedOrd {
		t.foldedOrd = ord + 1
	}
	if batchFrom >= 0 {
		t.dirtyFrom = min(t.dirtyFrom, batchFrom)
		t.unsaved++
	}

	total := t.length()
	pending := t.dirtyFrom < total || t.persistedOrd != t.foldedOrd
	due := settled || t.unsaved >= chatFoldPersistEvery || time.Since(t.lastSave) >= chatFoldPersistAfter
	if pending && due {
		a.persistChatTail(chatID, t, total)
	}

	if batchFrom >= 0 {
		busEmit(chatMessagesChangedPrefix+chatID, ChatMessagesChanged{
			FromIndex: batchFrom,
			Messages:  t.slice(batchFrom),
		})
	}
}

// persistChatTail is the transaction the phase's whole invariant rests on:
// the changed chat_messages rows and the folded_ord bump commit TOGETHER, so
// the marker never claims more than the stored transcript contains. That pair
// is exactly what trim reads to decide which stream lines are safe to delete.
//
// What this transaction deliberately does NOT cover, because
// chatStreamWriter.append is asynchronous by design (it assigns the ord
// synchronously and hands the row to the writer goroutine): the chat_stream
// row itself. If a crash loses a queued row after the fold already recorded
// its message, the transcript keeps the message and the stream lacks the line.
// That is the safe direction — a rendered message with no raw line, rather
// than a marker claiming a message nobody stored — and it is written down
// here rather than left for someone to discover.
//
// On failure NEITHER moves: no rows, no marker, and t's own bookkeeping is
// left untouched so the next attempt rewrites the same range. A crash then
// costs nothing, because trim still sees the old marker and keeps the stream
// lines the unpersisted messages came from.
//
// Called with t.mu held.
func (a *App) persistChatTail(chatID string, t *chatTail, total int) {
	if a.db == nil {
		return
	}
	if t.noPersist {
		// Adoption could not read the stored prefix, so this tail does not
		// know what row 0 is. Writing anything would prune the rows it failed
		// to load. See chatTail.noPersist.
		return
	}
	id, err := strconv.Atoi(chatID)
	if err != nil {
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		log.Printf("chat fold: begin %s: %v", chatID, err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO chat_messages (chat_id, ord, payload_json) VALUES (?, ?, ?)
		ON CONFLICT(chat_id, ord) DO UPDATE SET payload_json = excluded.payload_json`)
	if err != nil {
		log.Printf("chat fold: prepare %s: %v", chatID, err)
		return
	}
	defer stmt.Close()
	for i := max(t.dirtyFrom, 0); i < total; i++ {
		payload, err := json.Marshal(t.at(i))
		if err != nil {
			log.Printf("chat fold: encode %s#%d: %v", chatID, i, err)
			return
		}
		if _, err := stmt.Exec(id, i, string(payload)); err != nil {
			log.Printf("chat fold: upsert %s#%d: %v", chatID, i, err)
			return
		}
	}
	// The transcript can only shrink through a client's own edit, but the
	// delete is what keeps "rows 0..len-1" true rather than assumed.
	if _, err := tx.Exec(`DELETE FROM chat_messages WHERE chat_id = ? AND ord >= ?`, id, total); err != nil {
		log.Printf("chat fold: prune %s: %v", chatID, err)
		return
	}
	if _, err := tx.Exec(
		`INSERT INTO chat_stream_state (chat_id, folded_ord) VALUES (?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET folded_ord = MAX(folded_ord, excluded.folded_ord)`,
		chatID, t.foldedOrd,
	); err != nil {
		log.Printf("chat fold: folded_ord %s: %v", chatID, err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("chat fold: commit %s: %v", chatID, err)
		return
	}

	t.dirtyFrom = total
	t.persistedOrd = t.foldedOrd
	t.unsaved = 0
	t.lastSave = time.Now()
}

// chatTailFor returns the chat's tail, adopting the stored transcript on first
// touch in this process.
//
// The load happens OUTSIDE w.mu: it reads chat_messages, chat_stream_state and
// chat_stream, and w.mu is the ord allocator every append takes. Losing the
// insert race is fine — the winner loaded the same rows from the same tables,
// and its state is the one both goroutines then fold into.
func (a *App) chatTailFor(w *chatStreamWriter, chatID string, upTo int64) *chatTail {
	w.mu.Lock()
	t := w.tails[chatID]
	w.mu.Unlock()
	if t != nil {
		return t
	}

	loaded := a.loadChatTail(w, chatID, upTo)

	w.mu.Lock()
	defer w.mu.Unlock()
	if t := w.tails[chatID]; t != nil {
		return t
	}
	w.tails[chatID] = loaded
	return loaded
}

// loadChatTail is adoption. It reads the stored rows as the starting state and
// then folds forward from folded_ord only — never from ord 0, which for a
// trimmed chat would be a truncated history rather than a recomputed one.
func (a *App) loadChatTail(w *chatStreamWriter, chatID string, upTo int64) *chatTail {
	t := &chatTail{st: &foldState{messages: []ChatMessage{}}, lastSave: time.Now()}

	if id, err := strconv.Atoi(chatID); err == nil {
		raw, err := a.loadStoredChatMessages(id)
		var msgs []ChatMessage
		if err == nil {
			err = json.Unmarshal([]byte(raw), &msgs)
		}
		if err != nil {
			// A failed READ is not the same as an empty transcript, and
			// starting empty is the one thing that must not happen here: the
			// first persist would prune the rows we could not load. Fold in
			// memory so the live view still works, but never write.
			log.Printf("chat fold: adopt %s failed, transcript is read-only for this chat: %v", chatID, err)
			t.noPersist = true
		} else {
			t.adopted = msgs
		}
	}
	for _, m := range t.adopted {
		if m.ID+1 > t.idOffset {
			t.idOffset = m.ID + 1
		}
	}
	t.turnStart = len(t.adopted)
	t.dirtyFrom = len(t.adopted)

	// A stored row can only be partial if the app died mid-turn between a
	// coalesced write and the turn boundary. The turn it belonged to has ended
	// by definition, and an adopted row is never re-derived, so settling has
	// to happen here or the bubble renders as mid-stream forever. This is the
	// one thing adoption changes about the stored rows, and it changes only
	// the flag whose whole meaning is "still streaming".
	for i := range t.adopted {
		if t.adopted[i].Partial {
			settleTranscript(t.adopted[i:])
			t.dirtyFrom = i
			break
		}
	}

	folded, haveMarker := w.foldedOrd(chatID)
	t.foldedOrd = folded
	t.persistedOrd = folded

	if !haveMarker && len(t.adopted) > 0 {
		// Stored rows with NO marker: the config.json import and every client
		// save that passes foldedOrd = -1 write rows without one, and Go
		// appends to chat_stream for chats no client has mounted. Folding
		// [0, upTo) here would append a second copy of a transcript the
		// adopted rows already account for — permanently, on the next
		// persist. Absence of a marker is not evidence that nothing was
		// folded, so nothing is folded. (The frontend guarded exactly this:
		// chatSession.ts's `if (!folded) return`.)
		t.foldedOrd = max(t.foldedOrd, upTo)
		return t
	}

	// Catch up on lines recorded but not folded — a turn that was in flight
	// when the app last exited. Bounded above by the line being folded now, so
	// the live event is not applied twice (append() assigned its ord before
	// its INSERT, so it may already be in the table).
	for _, l := range a.loadChatStreamRange(chatID, folded, upTo) {
		for _, ev := range NormalizeChatLine(l.Kind, l.Line, l.Ord) {
			if isTurnBoundary(ev.Type) {
				settleTranscript(t.st.messages)
				t.turnStart = len(t.adopted) + len(t.st.messages)
				continue
			}
			applyChatEvent(t.st, ev)
		}
	}
	if upTo > t.foldedOrd {
		// Everything that existed in the window has been folded; a gap in it
		// is a line that is already gone, so holding the marker back would
		// only stop trim forever.
		t.foldedOrd = upTo
	}
	return t
}

// loadChatStreamRange returns the recorded lines in [from, upTo), oldest
// first. upTo < 0 means "no upper bound".
func (a *App) loadChatStreamRange(chatID string, from, upTo int64) []ChatStreamLine {
	if a.db == nil || (upTo >= 0 && upTo <= from) {
		return nil
	}
	query := `SELECT ord, kind, line FROM chat_stream WHERE chat_id = ? AND ord >= ? ORDER BY ord`
	args := []any{chatID, from}
	if upTo >= 0 {
		query = `SELECT ord, kind, line FROM chat_stream WHERE chat_id = ? AND ord >= ? AND ord < ? ORDER BY ord`
		args = append(args, upTo)
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		log.Printf("chat fold: catch-up %s: %v", chatID, err)
		return nil
	}
	defer rows.Close()
	var out []ChatStreamLine
	for rows.Next() {
		var l ChatStreamLine
		if err := rows.Scan(&l.Ord, &l.Kind, &l.Line); err != nil {
			log.Printf("chat fold: catch-up %s: %v", chatID, err)
			return out
		}
		out = append(out, l)
	}
	return out
}

// forgetChatTail drops a chat's in-memory transcript. Called when the chat is
// deleted (or when a client overwrites chat_messages wholesale, which is still
// possible until the clients move onto this fold), so the next line re-adopts
// from disk instead of folding onto state the table no longer agrees with —
// the same reason deleteChatStream clears nextOrd/appends.
func (w *chatStreamWriter) forgetChatTail(chatID string) {
	w.mu.Lock()
	delete(w.tails, chatID)
	w.mu.Unlock()
}

// chatTailMessages returns the in-memory transcript for a chat, or nil when
// this process has not folded it. The in-memory copy is authoritative while
// the app runs: the table lags it by up to one coalescing window.
func (w *chatStreamWriter) chatTailMessages(chatID string) []ChatMessage {
	w.mu.Lock()
	t := w.tails[chatID]
	w.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.noPersist {
		// This tail has no idea what the stored prefix is, so its combined
		// view would show a transcript that starts mid-conversation. Report
		// "nothing in memory" and let the caller read the table: stale but
		// true beats short and wrong, and the read may well succeed now.
		return nil
	}
	return t.combined()
}

// lockChatTail returns the chat's tail with ITS mu already held, or nil when
// this process has not folded the chat. The caller must Unlock it.
//
// This exists for one caller: SaveChatMessages, which is still a client-driven
// whole-transcript write until the clients move onto the fold. Without it, a
// client save and a coalesced persist can interleave — client commits N rows,
// the fold then upserts [dirtyFrom, len) and deletes >= len, leaving
// N..dirtyFrom-1 MISSING while MAX() keeps folded_ord at the higher value, at
// which point trim is free to delete the stream lines for the hole.
//
// Lock order note: this takes w.mu, RELEASES it, then takes t.mu — and
// forgetChatTail then retakes w.mu while t.mu is held. That is not a cycle,
// because no path anywhere holds w.mu while blocking on a t.mu (chatTailFor
// never touches t.mu; chatTailMessages releases w.mu first).
func (w *chatStreamWriter) lockChatTail(chatID string) *chatTail {
	w.mu.Lock()
	t := w.tails[chatID]
	w.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	return t
}
