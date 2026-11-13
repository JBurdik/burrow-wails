package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTurnNeeds(t *testing.T) {
	two := 1
	cases := []struct {
		name    string
		p       Params
		needs   string
		asks    int
		next    int
		wantErr bool
	}{
		{"asks imply you", Params{"asks": []any{map[string]any{"question": "Ship?", "options": []any{"Yes", "No"}, "suggested": float64(two)}}}, "you", 1, 0, false},
		{"CLI JSON string", Params{"needs": "you", "asks": `[{"question":"Which DB?"}]`}, "you", 1, 0, false},
		{"done with next", Params{"needs": "nothing", "next": []any{"Write tests", "", "Open PR", "Deploy", "Extra"}}, "nothing", 0, 3, false},
		{"done drops asks", Params{"needs": "nothing", "asks": `[{"question":"x"}]`}, "nothing", 0, 0, false},
		{"empty is done", Params{}, "nothing", 0, 0, false},
		{"you without asks", Params{"needs": "you"}, "", 0, 0, true},
		{"bad needs", Params{"needs": "maybe"}, "", 0, 0, true},
		{"bad json", Params{"asks": "[{"}, "", 0, 0, true},
	}
	for _, c := range cases {
		n, err := ParseTurnNeeds(c.p)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if err != nil {
			continue
		}
		if n.Needs != c.needs || len(n.Asks) != c.asks || len(n.Next) != c.next {
			t.Fatalf("%s: got %+v", c.name, n)
		}
	}
	n, _ := ParseTurnNeeds(Params{"asks": `[{"question":"A","options":["only"],"suggested":0},{"question":"B","options":["x","y"],"suggested":5}]`})
	if n.Asks[0].Options != nil || n.Asks[0].Suggested != -1 {
		t.Fatalf("single option should become free text: %+v", n.Asks[0])
	}
	if n.Asks[1].Suggested != -1 {
		t.Fatalf("out-of-range suggested should be dropped: %+v", n.Asks[1])
	}
}

func TestRunCommandProblem(t *testing.T) {
	if RunCommandProblem("npm run deploy -- --prod") != "" || RunCommandProblem("for f in *; do\n\techo $f\ndone") != "" {
		t.Fatal("plain commands must pass")
	}
	for _, bad := range []string{"", "  ", "ls‮", "echo a​", "echo \x1b[2J"} {
		if RunCommandProblem(bad) == "" {
			t.Fatalf("%q should be refused", bad)
		}
	}
	if _, err := ParseTurnNeeds(Params{"asks": `[{"question":"Deploy?","run":"rm x‮"}]`}); err == nil {
		t.Fatal("end_turn must refuse a hidden-character command")
	}
	n, err := ParseTurnNeeds(Params{"asks": `[{"question":"Deploy?","run":"make deploy"}]`})
	if err != nil || n.Asks[0].Run == nil || n.Asks[0].Run.Command != "make deploy" || n.Asks[0].Run.State != "" {
		t.Fatalf("run not kept: %+v %v", n, err)
	}
}

func TestTurnOptionDetails(t *testing.T) {
	n, err := ParseTurnNeeds(Params{"asks": []any{map[string]any{"question": "Next?", "options": []any{
		map[string]any{"label": "Highlights", "description": "RP panel with what the thread made"},
		"Review",
		map[string]any{"label": "", "description": "dropped: no label"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	a := n.Asks[0]
	if len(a.Options) != 2 || len(a.Details) != 2 || a.Details[0] != "RP panel with what the thread made" || a.Details[1] != "" {
		t.Fatalf("options %q details %q", a.Options, a.Details)
	}
	n, _ = ParseTurnNeeds(Params{"asks": `[{"question":"Q","options":["a","b"]}]`})
	if n.Asks[0].Details != nil {
		t.Fatalf("plain options must carry no details: %q", n.Asks[0].Details)
	}
}

type fakePins struct {
	chat         int64
	target, note string
}

func (f *fakePins) AddPin(chatID int64, target, note string, _ bool) (any, error) {
	f.chat, f.target, f.note = chatID, target, note
	return nil, nil
}

func TestPinVerbResolvesPaths(t *testing.T) {
	f := &fakePins{}
	c := New(Deps{Pins: f})
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, "out"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, "out", "mock.html"), []byte("<p>"), 0o644)
	if _, err := c.Call(context.Background(), ScopeLocal, "pin", Params{"parent_chat_id": float64(7), "target": "out/mock.html", "cwd": repo, "note": "look"}); err != nil {
		t.Fatal(err)
	}
	if f.chat != 7 || f.target != filepath.Join(repo, "out", "mock.html") || f.note != "look" {
		t.Fatalf("%+v", f)
	}
	_, _ = c.Call(context.Background(), ScopeLocal, "pin", Params{"parent_chat_id": float64(7), "target": "https://x.dev/a"})
	if f.target != "https://x.dev/a" {
		t.Fatalf("url changed: %q", f.target)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "pin", Params{"target": "/a"}); err == nil {
		t.Fatal("pin outside a chat must refuse")
	}
}

type fakeNeeds struct{ stored string }

func (f *fakeNeeds) SetTurnNeeds(_ int64, n TurnNeeds) error { f.stored = n.Needs; return nil }
func (f *fakeNeeds) TurnNeedsPending(int64) (bool, error)    { return f.stored == "", nil }

func TestEndTurnOncePerTurnAndInstructs(t *testing.T) {
	f := &fakeNeeds{}
	c := New(Deps{TurnNeeds: f})
	out, err := c.Call(context.Background(), ScopeLocal, "end_turn", Params{"parent_chat_id": float64(3), "needs": "nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := out.(map[string]any); !strings.Contains(fmt.Sprint(m["next_step"]), "write your final reply") {
		t.Fatalf("result must tell the agent to write its reply next: %v", out)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "end_turn", Params{"parent_chat_id": float64(3), "needs": "nothing"}); err == nil {
		t.Fatal("a second end_turn in the same turn must be refused")
	}
	n, _ := ParseTurnNeeds(Params{"asks": `[{"question":"Which?","options":["a","b","c"],"multi":true}]`})
	if !n.Asks[0].Multi {
		t.Fatal("multi not kept")
	}
}

func TestPinRefusesMissingFilesAndOtherSchemes(t *testing.T) {
	c := New(Deps{Pins: &fakePins{}})
	for _, target := range []string{"file:///etc/passwd", "/definitely/not/here.html"} {
		if _, err := c.Call(context.Background(), ScopeLocal, "pin", Params{"parent_chat_id": float64(7), "target": target}); err == nil {
			t.Fatalf("%q must be refused", target)
		}
	}
}
