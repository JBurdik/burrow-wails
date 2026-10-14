// Package daemonproto defines the newline-delimited JSON protocol spoken
// over the burrow-daemon Unix socket. PTY byte payloads travel as JSON
// number arrays (not strings/base64) end-to-end — matching what the
// frontend's write_pty/pty-data-{id} contract expects (arbitrary binary
// PTY output, not necessarily valid UTF-8) and avoiding any lossy
// re-encoding between the daemon and the app process.
package daemonproto

// Request is a client -> daemon message. Kind selects which fields matter.
type Request struct {
	ReqID string   `json:"reqId"`
	Kind  string   `json:"kind"` // "spawn" | "write" | "resize" | "kill" | "list" | "foreground" | "version" | "since"
	ID    string   `json:"id,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Env   []string `json:"env,omitempty"`
	Data  []int    `json:"data,omitempty"`
	Cols  uint16   `json:"cols,omitempty"`
	Rows  uint16   `json:"rows,omitempty"`
	Since uint64   `json:"since,omitempty"` // for "since"
}

// Response is a daemon -> client reply to a Request (matched by ReqID).
type Response struct {
	ReqID string   `json:"reqId"`
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	IDs   []string `json:"ids,omitempty"`  // for "list"
	Name  string   `json:"name,omitempty"` // for "foreground"
	// For "foreground", Pid is the foreground process group id (0 from a daemon
	// that predates the field — callers treat that as "unknown").
	// ExePath/Pid answer "version": the daemon's own binary path and pid, so a
	// client reconnecting to an ALREADY-RUNNING daemon.sock (the whole point of
	// the daemon surviving app restarts) can tell "still my binary" apart from
	// "orphaned process left over from a deleted build/worktree" and replace
	// the latter instead of silently reattaching to dead code forever.
	ExePath string `json:"exePath,omitempty"`
	Pid     int    `json:"pid,omitempty"`
	// For "since": the PTY's output from Off on (Off is later than the asked
	// offset when the daemon's ring no longer holds all of it).
	Off  uint64 `json:"off,omitempty"`
	Data []int  `json:"data,omitempty"`
}

// Frame is an unsolicited daemon -> client push: PTY output or exit.
type Frame struct {
	Event string `json:"event"` // "pty-data" | "pty-exit"
	ID    string `json:"id"`
	Data  []int  `json:"data,omitempty"`
	// Off is the offset of Data's first byte in the PTY's whole output. A
	// pointer because a daemon that predates it sends none, and 0 is a real
	// offset — the client must not dedupe on a number it was never given.
	Off *uint64 `json:"off,omitempty"`
}

// Envelope wraps either a Response or a Frame on the wire so a single
// decoder can tell them apart.
type Envelope struct {
	Type     string    `json:"type"` // "response" | "frame"
	Response *Response `json:"response,omitempty"`
	Frame    *Frame    `json:"frame,omitempty"`
}

// BytesToInts/IntsToBytes convert between raw PTY bytes and the wire's
// JSON-safe []int representation.
func BytesToInts(b []byte) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

func IntsToBytes(ints []int) []byte {
	out := make([]byte, len(ints))
	for i, v := range ints {
		out[i] = byte(v)
	}
	return out
}
