package control

import (
	"context"
	"errors"
	"testing"
)

type fakeScreen struct {
	text string
	err  error
}

func (f fakeScreen) ScreenText(string, int) (string, error) { return f.text, f.err }

// tab_output reads the daemon's emulator first — that is what makes it work
// with the window closed — and only falls back to the mounted xterm when the
// daemon has no emulator (built without `vt`, or an older daemon).
func TestTabOutputPrefersTheDaemonScreen(t *testing.T) {
	ui := &fakeUI{result: map[string]any{"text": "from ui"}}
	c := newTestCore(t, Deps{UI: ui, Screen: fakeScreen{text: "from daemon"}})
	out, err := c.Call(context.Background(), ScopeLocal, "tab_output", Params{"pty_id": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(map[string]any)["text"]; got != "from daemon" {
		t.Fatalf("text = %v, want the daemon's", got)
	}
	if ui.action != "" {
		t.Fatalf("UI was asked (%s) although the daemon answered", ui.action)
	}
}

func TestTabOutputFallsBackToTheUI(t *testing.T) {
	ui := &fakeUI{result: map[string]any{"text": "from ui"}}
	c := newTestCore(t, Deps{UI: ui, Screen: fakeScreen{err: errors.New("pty screen unsupported")}})
	out, err := c.Call(context.Background(), ScopeLocal, "tab_output", Params{"pty_id": float64(7), "lines": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(map[string]any)["text"]; got != "from ui" {
		t.Fatalf("text = %v, want the UI's", got)
	}
	if ui.action != "tab_output" || ui.args["lines"] != int64(5) {
		t.Fatalf("UI call = %s %v", ui.action, ui.args)
	}
}
