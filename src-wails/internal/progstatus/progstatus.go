// Package progstatus parses OSC 7501, the generic Program Status Protocol
// (https://www.superlogical.com/rex/docs/build/program-status.md, rev 0.4):
// any program writing to a PTY (cargo, terraform, a shell function) can say
// it is idle / working / blocked / done / failed, and why. Burrow feeds the
// ROOT record into the Go-derived PTY phase (agentphase.ProgramStatus).
//
// Pure: no IO, no main. Burrow never answers `OSC 7501 ; ?` — that silence is
// load-bearing, see Event.
package progstatus

import (
	"bytes"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"burrow/internal/agentphase"
)

// Limits from the spec. A report breaking one is discarded whole.
const (
	maxSeq      = 4096 // OSC through ST
	maxKey      = 16
	maxMsgEnc   = 2732
	maxMsgDec   = 2048
	maxTitleEnc = 256
	maxTitleDec = 192
	maxID       = 128
)

var intro = []byte("\x1b]7501;")

// Report is one parsed sequence. Only what Burrow uses is kept; title, app
// and progress are validated (a bad title still discards the report) and
// dropped. ponytail: add them to the struct when something displays them.
type Report struct {
	State string // idle | working | done | blocked | error | clear
	ID    string // "" = root record
	Kind  string // permission | question | auth | "" (blocked only)
	Msg   string // decoded, control-char free
}

// Parse parses the body between "OSC 7501;" and ST. ok=false means the
// report must be ignored (no/unknown state, a broken limit, bad base64,
// control characters, a malformed id). `?` (feature detection) has no state,
// so it lands here too.
func Parse(body string) (r Report, ok bool) {
	if len(intro)+len(body)+1 > maxSeq {
		return r, false
	}
	vals := map[string]string{}
	for _, pair := range strings.Split(body, ":") {
		k, v, found := strings.Cut(pair, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(k) > maxKey {
			return r, false
		}
		if !found || k == "" || !isKey(k) || !isValue(v) {
			continue // malformed pair: skip it, keep the rest
		}
		vals[k] = v // last one wins
	}
	r.State = vals["state"]
	switch r.State {
	case "idle", "working", "done", "blocked", "error", "clear":
	default:
		return Report{}, false
	}
	if id, has := vals["id"]; has {
		if !validID(id) {
			return Report{}, false
		}
		r.ID = id
	}
	if r.State == "blocked" {
		switch k := vals["kind"]; k {
		case "permission", "question", "auth":
			r.Kind = k
		}
	}
	if msg, has := vals["msg"]; has {
		if r.Msg, ok = decodeText(msg, maxMsgEnc, maxMsgDec); !ok {
			return Report{}, false
		}
	}
	if title, has := vals["title"]; has {
		if _, ok = decodeText(title, maxTitleEnc, maxTitleDec); !ok {
			return Report{}, false
		}
	}
	return r, true
}

func isKey(k string) bool {
	for i := 0; i < len(k); i++ {
		if k[i] < 'a' || k[i] > 'z' {
			return false
		}
	}
	return true
}

func isValue(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(isAlnum(c) || strings.IndexByte("_.,+/=-", c) >= 0) {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func validID(id string) bool {
	segs := strings.Split(id, "/")
	if len(id) > maxID || len(segs) > 8 {
		return false
	}
	for _, s := range segs {
		if len(s) < 1 || len(s) > 32 {
			return false
		}
		for i := 0; i < len(s); i++ {
			if !isAlnum(s[i]) && strings.IndexByte("_.+-", s[i]) < 0 {
				return false
			}
		}
	}
	return true
}

// decodeText: standard base64, padding optional, encoded size checked first.
func decodeText(enc string, maxEnc, maxDec int) (string, bool) {
	if len(enc) > maxEnc {
		return "", false
	}
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(enc, "="))
	if err != nil || len(b) > maxDec || !utf8.Valid(b) {
		return "", false
	}
	for _, c := range string(b) {
		if c < 0x20 || c >= 0x7f && c <= 0x9f {
			return "", false
		}
	}
	return string(b), true
}

// Event maps a report onto the PTY phase. Only the root record drives it.
//
// ponytail: child ids (`id=build/test`) are ignored — the phase is one value
// per tab, and folding a record tree into it (a blocked child under a working
// root) is a display decision nobody has made yet. Add a record set here when
// the UI wants per-record rows.
//
// Precedence with Claude's hooks / status file: none needed in practice.
// Claude Code knows OSC 7501 but probes with `OSC 7501 ; ?` and stays silent
// without a reply — and Burrow never replies. An agent that emits it anyway
// is the same process its hooks speak for: latest wins, and idle/clear can
// never erase an unseen done (see agentphase.ProgramStatus).
func Event(r Report) (agentphase.Event, bool) {
	if r.ID != "" {
		return agentphase.Event{}, false
	}
	ev := agentphase.Event{Kind: agentphase.ProgramStatus, Detail: r.Msg}
	switch r.State {
	case "working":
		ev.State = agentphase.Running
	case "blocked":
		ev.State = agentphase.WaitingInput
		if r.Kind == "permission" {
			ev.State = agentphase.WaitingApproval
		}
	case "done":
		ev.State = agentphase.Done
	case "error":
		ev.State = agentphase.Failed
	default: // idle, clear
		ev.State = agentphase.Idle
		ev.Detail = ""
	}
	return ev, true
}

// Scanner pulls OSC 7501 bodies out of a PTY byte stream, carrying a
// sequence split across reads to the next Feed. It buffers only from an
// intro (or a trailing prefix of one) on, at most maxSeq bytes. One per PTY.
type Scanner struct{ buf []byte }

func (s *Scanner) Feed(p []byte) (bodies []string) {
	b := p
	if len(s.buf) > 0 {
		b = append(s.buf, p...)
		s.buf = nil
	}
	for {
		i := bytes.Index(b, intro)
		if i < 0 {
			for k := min(len(intro)-1, len(b)); k > 0; k-- {
				if bytes.HasSuffix(b, intro[:k]) {
					s.buf = append([]byte(nil), b[len(b)-k:]...)
					break
				}
			}
			return bodies
		}
		rest := b[i+len(intro):]
		end, stLen := terminator(rest)
		switch {
		case end == -1: // incomplete: wait for more, unless already too long
			if len(b)-i < maxSeq {
				s.buf = append([]byte(nil), b[i:]...)
				return bodies
			}
			b = rest
		case end == -2: // aborted by another escape sequence
			b = rest
		default:
			if len(intro)+end+stLen <= maxSeq {
				bodies = append(bodies, string(rest[:end]))
			}
			b = rest[end+stLen:]
		}
	}
}

// terminator finds ST (BEL or ESC \) in b. -1 = not yet, -2 = an ESC that
// starts something else (the OSC is abandoned, as a terminal would).
func terminator(b []byte) (end, stLen int) {
	for i, c := range b {
		switch c {
		case 0x07:
			return i, 1
		case 0x1b:
			if i+1 == len(b) {
				return -1, 0
			}
			if b[i+1] == '\\' {
				return i, 2
			}
			return -2, 0
		}
	}
	return -1, 0
}
