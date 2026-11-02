package ptycore

import "sync"

// ringSize is how much of each PTY's output the daemon keeps for a client that
// lost some of it (a dropped /v2/ws — the phone on a train). Per PTY, in the
// daemon's memory.
//
// ponytail: fixed 1 MiB per pty; make it a setting if an outage outruns it often.
const ringSize = 1 << 20

// byteRing numbers every byte a PTY ever wrote (end = total written) and keeps
// the last `size` of them, so a client holding an offset can ask for exactly
// what it missed.
type byteRing struct {
	mu   sync.Mutex
	size int
	buf  []byte // the last min(end, size) bytes, oldest first
	end  uint64
}

func newByteRing(size int) *byteRing { return &byteRing{size: size} }

// write appends b and returns the offset of b's first byte.
func (r *byteRing) write(b []byte) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	off := r.end
	r.end += uint64(len(b))
	r.buf = append(r.buf, b...)
	if over := len(r.buf) - r.size; over > 0 {
		// ponytail: copy-down trim, O(ringSize) per chunk past the cap; a true
		// circular buffer if the daemon ever shows up in a profile.
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	return off
}

// since returns the bytes from offset `from` up to the current end, and the
// offset the returned bytes start at. That start is greater than `from` when
// the ring has already dropped some of them — the caller lost those for good.
// A `from` past the end (a client ahead of a restarted daemon) returns nothing
// at the end.
func (r *byteRing) since(from uint64) (uint64, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.end - uint64(len(r.buf))
	if from > r.end {
		return r.end, nil
	}
	if from < start {
		from = start
	}
	return from, append([]byte(nil), r.buf[from-start:]...)
}
