package control

import "testing"

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
