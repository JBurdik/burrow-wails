package main

import (
	"sync"
	"time"
)

// clientIDs makes chat_send / chat_queue_add idempotent: a client tags each
// typed message with a clientId and may retry it after a dropped socket (the
// first call may already have reached Go). An id counts as accepted only once
// the send / insert succeeded; a retry arriving while the first is still in
// flight waits for it.
//
// ponytail: in memory, so a core restart forgets every id — a retry that
// straddles a restart can deliver twice. Persist to SQLite if that matters.
type clientIDs struct {
	mu sync.Mutex
	m  map[int64]map[string]*clientIDEntry
}

type clientIDEntry struct {
	at   time.Time
	done chan struct{} // closed when the first call finished
	ok   bool
	item *ChatQueueItem // set when accepted by chat_queue_add
}

const (
	clientIDTTL = time.Hour
	clientIDCap = 500
)

// claim returns dup=true (with the accepted entry) when id was already
// accepted for chatID. Otherwise the caller owns the id and must call finish
// exactly once. An empty id never dedupes.
func (r *clientIDs) claim(chatID int64, id string) (prev *clientIDEntry, finish func(ok bool, item *ChatQueueItem), dup bool) {
	if id == "" {
		return nil, func(bool, *ChatQueueItem) {}, false
	}
	for {
		r.mu.Lock()
		if r.m == nil {
			r.m = map[int64]map[string]*clientIDEntry{}
		}
		ids := r.m[chatID]
		if ids == nil {
			ids = map[string]*clientIDEntry{}
			r.m[chatID] = ids
		}
		e := ids[id]
		if e == nil {
			r.pruneLocked(ids)
			e = &clientIDEntry{at: time.Now(), done: make(chan struct{})}
			ids[id] = e
			r.mu.Unlock()
			return nil, func(ok bool, item *ChatQueueItem) {
				r.mu.Lock()
				e.ok, e.item = ok, item
				if !ok {
					delete(ids, id)
				}
				r.mu.Unlock()
				close(e.done)
			}, false
		}
		r.mu.Unlock()
		<-e.done
		r.mu.Lock()
		ok := e.ok
		r.mu.Unlock()
		if ok {
			return e, nil, true
		}
		// The first attempt failed and released the id: try to own it.
	}
}

// pruneLocked drops expired ids and, past the cap, the oldest finished ones.
func (r *clientIDs) pruneLocked(ids map[string]*clientIDEntry) {
	cut := time.Now().Add(-clientIDTTL)
	for k, e := range ids {
		if e.at.Before(cut) && e.ok {
			delete(ids, k)
		}
	}
	for len(ids) >= clientIDCap {
		var oldK string
		var old *clientIDEntry
		for k, e := range ids {
			if e.ok && (old == nil || e.at.Before(old.at)) {
				oldK, old = k, e
			}
		}
		if old == nil {
			return
		}
		delete(ids, oldK)
	}
}
