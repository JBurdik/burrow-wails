package main

import (
	"testing"

	"burrow/internal/control"
)

func TestComposeTurnAnswer(t *testing.T) {
	one := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{{Question: "Ship?", Options: []string{"Yes", "No"}}}}
	three := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{
		{Question: "DB?", Options: []string{"Postgres", "SQLite"}},
		{Question: "Langs?", Options: []string{"Go", "TS", "Swift"}, Multi: true},
		{Question: "Name?\nshort"},
	}}
	run := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{
		{Question: "Ship?", Options: []string{"Yes"}},
		{Question: "Run the migration", Run: &control.TurnRun{Command: "make migrate", State: "done", Reply: " migrated 3 "}},
	}}
	done := control.TurnNeeds{Needs: "nothing", Next: []string{"Open PR", "Add tests", "Deploy"}}

	cases := []struct {
		name  string
		n     control.TurnNeeds
		picks []TurnPick
		extra string
		want  string
		err   bool
	}{
		{"single ask = just the answer + extra", one, []TurnPick{{Index: 0, Options: []string{"Yes"}}}, " also bump ", "Yes\n\nalso bump", false},
		{"free text, unanswered left out, quoted multiline", three,
			[]TurnPick{{Index: 0, Options: []string{"Postgres"}}, {Index: 2, Text: " api "}}, "",
			"> DB?\nPostgres\n\n> Name?\n> short\napi", false},
		{"multi in card order", three, []TurnPick{{Index: 1, Options: []string{"Swift", "Go"}}}, "",
			"> Langs?\nGo, Swift", false},
		{"run reply answers its ask", run, []TurnPick{{Index: 0, Options: []string{"Yes"}}}, "",
			"> Ship?\nYes\n\n> Run the migration\nmigrated 3", false},
		{"run reply alone", run, nil, "", "> Run the migration\nmigrated 3", false},
		{"extra only", one, nil, "just this", "just this", false},
		{"unknown option", one, []TurnPick{{Index: 0, Options: []string{"Maybe"}}}, "", "", true},
		{"two options on a single ask", one, []TurnPick{{Index: 0, Options: []string{"Yes", "No"}}}, "", "", true},
		{"bad index", one, []TurnPick{{Index: 3, Text: "x"}}, "", "", true},
		{"nothing at all", one, nil, " ", "", true},
		{"one follow-up", done, []TurnPick{{Options: []string{"Open PR"}}}, "", "Open PR", false},
		{"follow-ups as list + extra", done, []TurnPick{{Options: []string{"Deploy", "Open PR"}}}, "thanks", "- Open PR\n- Deploy\n\nthanks", false},
		{"asked only", done, []TurnPick{{Options: []string{"Deploy"}, Ask: true}}, "", "What do you mean by: Deploy?", false},
		{"ticked + asked + extra", done, []TurnPick{{Options: []string{"Open PR"}}, {Options: []string{"Deploy", "Add tests"}, Ask: true}}, "go",
			"Open PR\n\nWhat do you mean by: Add tests?\nWhat do you mean by: Deploy?\n\ngo", false},
		{"unknown asked follow-up", done, []TurnPick{{Options: []string{"Nope"}, Ask: true}}, "", "", true},
		{"unknown follow-up", done, []TurnPick{{Options: []string{"Nope"}}}, "", "", true},
	}
	for _, c := range cases {
		got, err := composeTurnAnswer(c.n, c.picks, c.extra)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%s: got %q, %v; want %q (err %v)", c.name, got, err, c.want, c.err)
		}
	}
}

// The door end to end: composed from the stored card, sent, clientId dedupes;
// no card → refused.
func TestChatAnswerTurnSubmits(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	if _, err := a.ChatAnswerTurn(c.ID, []TurnPick{{Index: 0, Text: "x"}}, "", nil, "a-0"); err == nil {
		t.Fatal("answer with no card: want an error")
	}
	if err := a.SetTurnNeeds(c.ID, control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{{Question: "Ship?", Options: []string{"Yes", "No"}}}}); err != nil {
		t.Fatal(err)
	}
	res, err := a.ChatAnswerTurn(c.ID, []TurnPick{{Index: 0, Options: []string{"No"}}}, "wait", nil, "a-1")
	if err != nil || !res.Sent || res.Text != "No\n\nwait" {
		t.Fatalf("answer = %+v, %v; want sent with the composed text", res, err)
	}
	if n := countSent(fake.sent, `No\n\nwait`); n != 1 {
		t.Fatalf("sent %d times", n)
	}
	if _, err := a.ChatAnswerTurn(c.ID, []TurnPick{{Index: 0, Options: []string{"No"}}}, "wait", nil, "a-1"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := countSent(fake.sent, `No\n\nwait`); n != 1 {
		t.Fatalf("retry sent again (%d)", n)
	}
}
