package ptycore

import (
	"bytes"
	"testing"
)

func TestByteRingOffsets(t *testing.T) {
	r := newByteRing(8)
	if off := r.write([]byte("abc")); off != 0 {
		t.Fatalf("first write off = %d, want 0", off)
	}
	if off := r.write([]byte("defg")); off != 3 {
		t.Fatalf("second write off = %d, want 3", off)
	}
	start, b := r.since(2)
	if start != 2 || string(b) != "cdefg" {
		t.Fatalf("since(2) = %d %q", start, b)
	}
	start, b = r.since(7)
	if start != 7 || len(b) != 0 {
		t.Fatalf("since(end) = %d %q, want nothing", start, b)
	}
}

func TestByteRingFellOut(t *testing.T) {
	r := newByteRing(4)
	r.write([]byte("abcdef")) // keeps "cdef", offsets 2..5
	r.write([]byte("gh"))     // keeps "efgh", offsets 4..7
	start, b := r.since(1)
	if start != 4 || string(b) != "efgh" {
		t.Fatalf("since(1) = %d %q, want 4 efgh", start, b)
	}
	start, b = r.since(6)
	if start != 6 || string(b) != "gh" {
		t.Fatalf("since(6) = %d %q", start, b)
	}
}

func TestByteRingAheadOfEnd(t *testing.T) {
	r := newByteRing(4)
	r.write([]byte("ab"))
	start, b := r.since(100)
	if start != 2 || len(b) != 0 {
		t.Fatalf("since(100) = %d %q, want 2 nothing", start, b)
	}
}

func TestByteRingReturnsCopy(t *testing.T) {
	r := newByteRing(4)
	r.write([]byte("abcd"))
	_, b := r.since(0)
	r.write([]byte("xy"))
	if !bytes.Equal(b, []byte("abcd")) {
		t.Fatalf("since result aliased the ring: %q", b)
	}
}
