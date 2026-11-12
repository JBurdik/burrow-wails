//go:build vt

package ptycore

// The daemon-side terminal emulator (docs/plans/010-server-side-vt.md): every
// PTY's bytes also go through libghostty-vt, so the daemon knows what is on
// screen with no UI attached. Behind the `vt` build tag while it is in beta;
// vt_off.go is the ring-only twin. All libghostty use lives in this file, so an
// upstream API bump (no stability promise yet) touches one place.

import (
	"strings"

	g "go.mitchellh.com/libghostty"
)

// Caps per PTY (plan 010 "Decisions"). ponytail: fixed; a setting only if asked.
const (
	vtScrollbackLines = 10_000
	vtScrollbackBytes = 8 << 20
)

// Emulator: this build keeps a terminal emulator per PTY (pty_attach works).
func Emulator() bool { return true }

// screen is NOT safe for concurrent use; session.vtMu serialises every call.
// No WritePty / clipboard effect is registered on purpose: xterm.js already
// answers DA/DSR queries, and two emulators answering = doubled replies.
type screen struct{ t *g.Terminal }

func newScreen(cols, rows uint16) *screen {
	t, err := g.NewTerminal(
		g.WithSize(max(cols, 1), max(rows, 1)),
		g.WithMaxScrollbackLines(vtScrollbackLines),
		g.WithMaxScrollbackBytes(vtScrollbackBytes),
		g.WithContinuationMaxBytes(4096),
	)
	if err != nil {
		return nil // the PTY still works, ring-only; screen ops answer unsupported
	}
	return &screen{t: t}
}

func (s *screen) write(b []byte) { s.t.VTWrite(b) }

func (s *screen) resize(cols, rows uint16) {
	_ = s.t.Resize(max(cols, 1), max(rows, 1), 0, 0)
}

func (s *screen) close() { s.t.Close() }

func (s *screen) compress() { _, _ = s.t.Compress(g.TerminalCompressionIncremental) }

// text is the active screen (+ its scrollback) as plain text: soft wraps
// joined, rows right-trimmed, trailing blank rows dropped, last `lines` rows.
// Same contract as XTerm.vue's readOutput, which it replaces for tab_output.
func (s *screen) text(lines int) (string, error) {
	f, err := g.NewFormatter(s.t, g.WithFormatterFormat(g.FormatterFormatPlain),
		g.WithFormatterUnwrap(true), g.WithFormatterTrim(true))
	if err != nil {
		return "", err
	}
	defer f.Close()
	out, err := f.FormatString()
	if err != nil {
		return "", err
	}
	rows := strings.Split(strings.TrimRight(out, " \n"), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	if lines > 0 && len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	return strings.Join(rows, "\n"), nil
}

// snapshot is VT bytes that rebuild the active screen, its history, modes,
// cursor and style in a fresh emulator, plus the replay-safe tail of an
// unfinished escape sequence. Only the ACTIVE screen: with the alt screen up
// the primary is unreachable (Phase 0, Q2), so the caller re-attaches after
// the TUI exits.
func (s *screen) snapshot() (vt []byte, cols, rows uint16, alt bool, err error) {
	f, err := g.NewFormatter(s.t, g.WithFormatterFormat(g.FormatterFormatVT),
		g.WithFormatterExtraPalette(true), g.WithFormatterExtraModes(true),
		g.WithFormatterExtraScrollingRegion(true), g.WithFormatterExtraTabstops(true),
		g.WithFormatterExtraKeyboard(true), g.WithFormatterExtraCursor(true),
		g.WithFormatterExtraStyle(true), g.WithFormatterExtraHyperlink(true),
		g.WithFormatterExtraProtection(true), g.WithFormatterExtraKittyKeyboard(true),
		g.WithFormatterExtraCharsets(true))
	if err != nil {
		return nil, 0, 0, false, err
	}
	defer f.Close()
	if vt, err = f.Format(); err != nil {
		return nil, 0, 0, false, err
	}
	cont, _ := s.t.Continuation()
	vt = append(vt, cont...)
	cols, _ = s.t.Cols()
	rows, _ = s.t.Rows()
	return vt, cols, rows, s.alt(), nil
}

// atPrompt: the cursor sits on a shell prompt the shell marked itself (OSC 133
// semantic prompts — fish, nushell, any shell with integration). Never true
// for a shell that emits no marks.
func (s *screen) atPrompt() bool {
	ok, _ := s.t.CursorAtPrompt()
	return ok
}

func (s *screen) alt() bool {
	scr, _ := s.t.ActiveScreen()
	return scr == g.ScreenAlternate
}
