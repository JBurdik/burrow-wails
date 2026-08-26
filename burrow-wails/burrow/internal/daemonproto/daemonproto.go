// Package daemonproto defines the newline-delimited JSON protocol spoken
// over the burrow-daemon Unix socket, mirroring the Rust backend's
// daemon_client.rs wire format in spirit (spawn/write/resize/kill/list +
// streamed pty-data/pty-exit frames), but reimplemented for Go.
package daemonproto

// Request is a client -> daemon message. Kind selects which fields matter.
type Request struct {
	ReqID string   `json:"reqId"`
	Kind  string   `json:"kind"` // "spawn" | "write" | "resize" | "kill" | "list"
	ID    string   `json:"id,omitempty"`
	Shell string   `json:"shell,omitempty"`
	Args  []string `json:"args,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Env   []string `json:"env,omitempty"`
	Data  string   `json:"data,omitempty"`
	Cols  uint16   `json:"cols,omitempty"`
	Rows  uint16   `json:"rows,omitempty"`
}

// Response is a daemon -> client reply to a Request (matched by ReqID).
type Response struct {
	ReqID string   `json:"reqId"`
	OK    bool     `json:"ok"`
	Error string   `json:"error,omitempty"`
	ID    string   `json:"id,omitempty"`   // for "spawn"
	IDs   []string `json:"ids,omitempty"`  // for "list"
}

// Frame is an unsolicited daemon -> client push: PTY output or exit.
type Frame struct {
	Event string `json:"event"` // "pty-data" | "pty-exit"
	ID    string `json:"id"`
	Data  string `json:"data,omitempty"`
}

// Envelope wraps either a Response or a Frame on the wire so a single
// decoder can tell them apart.
type Envelope struct {
	Type     string    `json:"type"` // "response" | "frame"
	Response *Response `json:"response,omitempty"`
	Frame    *Frame    `json:"frame,omitempty"`
}
