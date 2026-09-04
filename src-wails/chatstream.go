package main

import (
	"database/sql"
	"log"
	"sync"
)

// Live chat stream log. Phase 1 of docs/plans/003-view-state-routes.md.
//
// chat_messages holds the *rendered* transcript, written once per turn by the
// frontend. That is fine while a chat component stays mounted forever, but it
// is not enough to unmount one: deltas emitted while nothing is listening are
// gone, because emitAll is fire-and-forget. So every raw agent line also lands
// here, append-only under a monotonic per-chat `ord`, and can be replayed from
// any point.
//
// The shape is the bottom half of t3code's event sourcing (`orchestration_events`
// + `sequence`). Deliberately without its top half: no server-side projector.
// The frontend reducer stays the only thing that turns raw lines into messages,
// so there is no parsing logic to keep in sync across two languages.
//
// ponytail: rows are the transport's own lines, opaque. Nothing server-side
// looks inside them, and both transports (Claude stream-json, ACP JSON-RPC)
// fit the same columns.

const (
	// chatStreamQueue is the backlog tolerated before lines get dropped. A
	// chat's own turn is bursty but small; this is minutes of the loudest
	// output we have seen.
	chatStreamQueue = 4096
	// chatStreamKeep bounds one chat's log. Replay only ever needs what a
	// mounted component missed, plus enough to rebuild the turn in flight
	// after a crash — not the chat's whole history, which chat_messages
	// already holds.
	//
	// ponytail: trim by row count, not by turn boundary. The backend does not
	// parse lines, so it cannot see turns; counting is the only cheap ceiling.
	// If replay ever comes up short, trim on turn boundaries instead.
	chatStreamKeep = 20000
	// chatStreamHardKeep is the ceiling that applies even to lines nobody has
	// folded yet. Without it a chat that never saves its transcript grows the
	// DB forever; with it, such a chat loses its oldest lines instead. Losing
	// them is bad, so the limit is far above anything a real session reaches —
	// it is a backstop against a bug, not a routine trim.
	chatStreamHardKeep = 200000
	// chatStreamTrimEvery keeps the DELETE off the hot path — one trim per
	// this many appends per chat.
	chatStreamTrimEvery = 1000
)

func chatStreamSchema() []string {
	return []string{
		// folded_ord = the first ord the frontend has NOT yet folded into
		// chat_messages. This is t3code's projection_state.last_applied_sequence,
		// and the only reason it exists is the trim: without it the trim is a
		// blind cut that can delete lines nobody has rendered yet, so a restart
		// finds a hole in the middle of a turn. We take the column, not their
		// server-side projector.
		`CREATE TABLE IF NOT EXISTS chat_stream_state (
			chat_id TEXT PRIMARY KEY,
			folded_ord INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_stream (
			chat_id TEXT NOT NULL,
			ord INTEGER NOT NULL,
			kind TEXT NOT NULL,
			line TEXT NOT NULL,
			PRIMARY KEY (chat_id, ord)
		)`,
	}
}

// ChatStreamLine is one recorded agent line. `Kind` is the event topic minus
// the chat id ("claude-data", "acp-data", "acp-req"): a chat listens on more
// than one channel, and a replay has to put each line back on the one it came
// from. Ord is shared across kinds, so their relative order survives too.
type ChatStreamLine struct {
	Ord  int64  `json:"ord"`
	Kind string `json:"kind"`
	Line string `json:"line"`
}

type chatStreamRow struct {
	chatID string
	line   ChatStreamLine
}

// chatStreamWriter serialises appends onto one goroutine so the stream path
// never waits on SQLite. Ords are handed out synchronously (under mu) rather
// than in the writer, so the caller can emit the ord alongside the line and a
// remounting client can dedupe against it.
type chatStreamWriter struct {
	db *sql.DB
	ch chan chatStreamRow

	mu      sync.Mutex
	nextOrd map[string]int64
	appends map[string]int
	dropped int
}

func newChatStreamWriter(db *sql.DB) *chatStreamWriter {
	w := &chatStreamWriter{
		db:      db,
		ch:      make(chan chatStreamRow, chatStreamQueue),
		nextOrd: map[string]int64{},
		appends: map[string]int{},
	}
	go w.run()
	return w
}

func (a *App) chatStream() *chatStreamWriter {
	if a.db == nil {
		return nil
	}
	a.streamOnce.Do(func() { a.streamW = newChatStreamWriter(a.db) })
	return a.streamW
}

// append records one raw agent line and returns its ord. A full queue
// drops the line rather than stalling the agent's output — a gap in replay is
// recoverable, a stalled stream is not.
func (w *chatStreamWriter) append(chatID, kind, line string) int64 {
	w.mu.Lock()
	ord, ok := w.nextOrd[chatID]
	if !ok {
		ord = w.loadMaxOrdLocked(chatID)
	}
	w.nextOrd[chatID] = ord + 1
	w.mu.Unlock()

	select {
	case w.ch <- chatStreamRow{chatID: chatID, line: ChatStreamLine{Ord: ord, Kind: kind, Line: line}}:
	default:
		w.mu.Lock()
		w.dropped++
		n := w.dropped
		w.mu.Unlock()
		if n == 1 || n%1000 == 0 {
			log.Printf("chat stream: write queue full, dropped %d line(s)", n)
		}
	}
	return ord
}

// loadMaxOrdLocked resumes a chat's numbering across app restarts, so a replay
// after a crash sees one continuous sequence. Runs once per chat per run.
func (w *chatStreamWriter) loadMaxOrdLocked(chatID string) int64 {
	var max sql.NullInt64
	if err := w.db.QueryRow(`SELECT MAX(ord) FROM chat_stream WHERE chat_id = ?`, chatID).Scan(&max); err != nil {
		log.Printf("chat stream: max ord for %s: %v", chatID, err)
		return 0
	}
	if !max.Valid {
		return 0
	}
	return max.Int64 + 1
}

func (w *chatStreamWriter) run() {
	for row := range w.ch {
		if _, err := w.db.Exec(
			`INSERT OR REPLACE INTO chat_stream (chat_id, ord, kind, line) VALUES (?, ?, ?, ?)`,
			row.chatID, row.line.Ord, row.line.Kind, row.line.Line,
		); err != nil {
			log.Printf("chat stream: append %s#%d: %v", row.chatID, row.line.Ord, err)
			continue
		}
		w.mu.Lock()
		w.appends[row.chatID]++
		due := w.appends[row.chatID] >= chatStreamTrimEvery
		if due {
			w.appends[row.chatID] = 0
		}
		w.mu.Unlock()
		if due {
			w.trim(row.chatID, row.line.Ord)
		}
	}
}

// trim drops lines that are both old and already folded into chat_messages.
// "Already folded" is the binding constraint: an unfolded line is the only copy
// of that part of the transcript, so age alone must never delete it.
func (w *chatStreamWriter) trim(chatID string, latestOrd int64) {
	cutoff := latestOrd - chatStreamKeep
	if folded := w.foldedOrd(chatID) - 1; folded < cutoff {
		cutoff = folded
	}
	if hard := latestOrd - chatStreamHardKeep; hard > cutoff {
		cutoff = hard
	}
	if cutoff < 0 {
		return
	}
	if _, err := w.db.Exec(
		`DELETE FROM chat_stream WHERE chat_id = ? AND ord <= ?`, chatID, cutoff,
	); err != nil {
		log.Printf("chat stream: trim %s: %v", chatID, err)
	}
}

func (w *chatStreamWriter) foldedOrd(chatID string) int64 {
	var ord int64
	err := w.db.QueryRow(`SELECT folded_ord FROM chat_stream_state WHERE chat_id = ?`, chatID).Scan(&ord)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("chat stream: folded ord for %s: %v", chatID, err)
	}
	return ord
}

// emitChatLine is the single door for agent output: persist, then emit. Both
// the Claude stream-json runtime and the ACP runtime go through it, so an
// unmounted chat behaves the same whichever agent is talking.
//
// The payload is {ord, line} rather than the bare line, because a client has
// to be able to say how far it has folded the stream into its transcript
// (chat_stream_state.folded_ord). Inferring that server-side would mean
// assuming every emitted line was actually processed — false across a crash,
// and the trim would then delete lines nobody rendered.
func (a *App) emitChatLine(chatID, kind, line string) {
	var ord int64 = -1
	if w := a.chatStream(); w != nil {
		ord = w.append(chatID, kind, line)
	}
	emitAll(a.ctx, kind+"-"+chatID, ChatStreamLine{Ord: ord, Kind: kind, Line: line})

	// Also publish the provider-neutral reading of the line (providerruntime.go).
	// Running alongside the raw channel rather than replacing it: the desktop
	// still reduces raw lines, so this can be adopted one consumer at a time
	// instead of in one flip. The remote client is the first that wants it — it
	// has been re-implementing the protocol to a shallower depth.
	if events := NormalizeChatLine(kind, line); len(events) > 0 {
		emitAll(a.ctx, "chat-event-"+chatID, ChatEventBatch{Ord: ord, Events: events})
	}
}

// ChatEventBatch is one raw line's worth of domain events. Batched rather than
// emitted one by one so their order, and the ord they came from, survive.
type ChatEventBatch struct {
	Ord    int64                  `json:"ord"`
	Events []ProviderRuntimeEvent `json:"events"`
}

// LoadChatEventsSince replays the recorded stream as domain events — the same
// catch-up LoadChatStreamSince offers, for a client that does not want to know
// any provider's wire format.
func (a *App) LoadChatEventsSince(chatID string, since int64) ([]ChatEventBatch, error) {
	lines, err := a.LoadChatStreamSince(chatID, since)
	if err != nil {
		return nil, err
	}
	out := []ChatEventBatch{}
	for _, l := range lines {
		if events := NormalizeChatLine(l.Kind, l.Line); len(events) > 0 {
			out = append(out, ChatEventBatch{Ord: l.Ord, Events: events})
		}
	}
	return out, nil
}

// LoadChatStreamSince returns the raw lines recorded at ord >= since, oldest
// first — what a chat missed while it was unmounted. Bound to the frontend for
// phase 2 (replay on mount).
func (a *App) LoadChatStreamSince(chatID string, since int64) ([]ChatStreamLine, error) {
	if a.db == nil {
		return nil, nil
	}
	rows, err := a.db.Query(
		`SELECT ord, kind, line FROM chat_stream WHERE chat_id = ? AND ord >= ? ORDER BY ord`,
		chatID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatStreamLine{}
	for rows.Next() {
		var l ChatStreamLine
		if err := rows.Scan(&l.Ord, &l.Kind, &l.Line); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ChatFoldedOrd is where a client resumes replay from after an app restart:
// everything below it is already in chat_messages.
func (a *App) ChatFoldedOrd(chatID string) (int64, error) {
	if a.db == nil {
		return 0, nil
	}
	var ord int64
	err := a.db.QueryRow(`SELECT folded_ord FROM chat_stream_state WHERE chat_id = ?`, chatID).Scan(&ord)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return ord, err
}

// deleteChatStream drops a chat's log. Called from DeleteChatMessages so a
// deleted chat leaves nothing behind and the frontend keeps one delete call.
func (a *App) deleteChatStream(chatID string) error {
	if a.db == nil {
		return nil
	}
	if _, err := a.db.Exec(`DELETE FROM chat_stream_state WHERE chat_id = ?`, chatID); err != nil {
		return err
	}
	if w := a.chatStream(); w != nil {
		w.mu.Lock()
		delete(w.nextOrd, chatID)
		delete(w.appends, chatID)
		w.mu.Unlock()
	}
	_, err := a.db.Exec(`DELETE FROM chat_stream WHERE chat_id = ?`, chatID)
	return err
}
