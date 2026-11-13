package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"burrow/internal/agentphase"
	"burrow/internal/control"
)

func TestLooseEnds(t *testing.T) {
	if got := looseEnds(looseEndFacts{Unpushed: -1, PrNumber: 3, PrState: "merged"}); len(got) != 0 {
		t.Fatalf("clean chat should have no loose ends, got %+v", got)
	}
	got := looseEnds(looseEndFacts{
		Uncommitted: 2, Unpushed: 1, PrNumber: 7, PrState: "open", Schedules: 1,
		Children: []looseChild{{ID: 9, Title: "Mine repo", State: agentphase.Running}, {ID: 10, State: agentphase.Done}, {ID: 11, State: agentphase.WaitingApproval}},
	})
	want := []string{"uncommitted", "unpushed", "pr", "subagent", "subagent", "schedule"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, k := range want {
		if got[i].Kind != k {
			t.Fatalf("item %d: kind %q, want %q (%+v)", i, got[i].Kind, k, got)
		}
	}
	if got[0].Text != "2 uncommitted files" || got[1].Text != "1 commit not pushed" || got[3].ChatID != 9 || got[4].Text != "Sub-agent 11 — waiting for you" {
		t.Fatalf("texts: %+v", got)
	}
}

func TestTurnNeedsClearedOnNewTurn(t *testing.T) {
	a := newTestApp(t)
	c, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	id := c.ID
	needs := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{{Question: "Ship?", Suggested: -1}}}
	if err := a.SetTurnNeeds(id, needs); err != nil {
		t.Fatal(err)
	}
	got, _ := a.chatByID(id)
	if got.TurnNeeds == "" {
		t.Fatal("turn_needs not stored")
	}
	cid := strconv.FormatInt(id, 10)
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookRunning})
	got, _ = a.chatByID(id)
	if got.TurnNeeds != "" {
		t.Fatalf("turn_needs should clear on a new turn, got %q", got.TurnNeeds)
	}
}

func TestTurnNeedsSurvivesMidTurnPhases(t *testing.T) {
	a := newTestApp(t)
	store, _ := NewPhaseStore(a.db)
	a.phases = store
	c, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	cid := strconv.FormatInt(c.ID, 10)
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookRunning})
	// end_turn arrives mid-turn; later lines of the same turn must not clear it.
	_ = a.SetTurnNeeds(c.ID, control.TurnNeeds{Needs: "nothing"})
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookRunning})
	a.applyChatPhase(cid, agentphase.Event{Kind: agentphase.HookDone})
	got, _ := a.chatByID(c.ID)
	if got.TurnNeeds == "" {
		t.Fatal("turn_needs cleared inside its own turn")
	}
}

func TestChatRunAskRunsStoredCommandOnce(t *testing.T) {
	a := newTestApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	ran := make(chan string, 2)
	prev := runAskShell
	runAskShell = func(_ context.Context, _ string, cmd string) (string, int) {
		ran <- cmd
		return "migrated 3 tables\n", 0
	}
	t.Cleanup(func() { runAskShell = prev })

	needs := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{
		{Question: "Run the prod migration?", Suggested: -1, Run: &control.TurnRun{Command: "make migrate-prod"}},
		{Question: "No command here", Suggested: -1},
	}}
	if err := a.SetTurnNeeds(c.ID, needs); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatRunAsk(c.ID, 1); err == nil {
		t.Fatal("an ask without a command must refuse")
	}
	if err := a.ChatRunAsk(c.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.ChatRunAsk(c.ID, 0); err == nil {
		t.Fatal("a second press must refuse")
	}
	if got := <-ran; got != "make migrate-prod" {
		t.Fatalf("ran %q", got)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		row, _ := a.chatByID(c.ID)
		if strings.Contains(row.TurnNeeds, `"state":"done"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("card never marked done: %s", row.TurnNeeds)
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, err := a.ChatQueueList(c.ID)
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Text, "migrated 3 tables") || !strings.Contains(items[0].Text, "exit 0") {
		t.Fatalf("queued reply: %+v %v", items, err)
	}
}

func TestEndTurnGate(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type, Command string
			}
		}
	}
	if err := json.Unmarshal([]byte(endTurnGateSettings("/app/My Burrow/bin/burrow")), &s); err != nil {
		t.Fatal(err)
	}
	if h := s.Hooks["Stop"]; len(h) != 1 || len(h[0].Hooks) != 1 || h[0].Hooks[0].Command != "'/app/My Burrow/bin/burrow' end-turn-gate" {
		t.Fatalf("stop hook: %+v", s)
	}

	a := newTestApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "t"})
	if p, _ := a.TurnNeedsPending(c.ID); !p {
		t.Fatal("a turn without end_turn is pending")
	}
	_ = a.SetTurnNeeds(c.ID, control.TurnNeeds{Needs: "nothing"})
	if p, _ := a.TurnNeedsPending(c.ID); p {
		t.Fatal("end_turn called: not pending")
	}
}
