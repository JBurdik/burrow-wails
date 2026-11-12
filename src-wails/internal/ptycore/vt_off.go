//go:build !vt

package ptycore

// Ring-only build: no emulator. newScreen returns nil, so every screen op
// answers ErrUnsupported and the caller falls back (tab_output → the UI,
// attach → cold ring replay). See vt.go.

func Emulator() bool { return false }

type screen struct{}

func newScreen(cols, rows uint16) *screen { return nil }

func (*screen) write([]byte)             {}
func (*screen) resize(_, _ uint16)       {}
func (*screen) close()                   {}
func (*screen) compress()                {}
func (*screen) text(int) (string, error) { return "", ErrUnsupported }
func (*screen) snapshot() ([]byte, uint16, uint16, bool, error) {
	return nil, 0, 0, false, ErrUnsupported
}
func (*screen) alt() bool      { return false }
func (*screen) atPrompt() bool { return false }
