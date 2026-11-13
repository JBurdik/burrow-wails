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
	// HookDone settles the turn audit on a goroutine; let it land before the
	// TempDir is removed (a late write recreated the WAL mid-cleanup).
	time.Sleep(100 * time.Millisecond)
}

func TestChatRunAskRunsStoredCommandOnce(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "t"})
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
	// Two asks on the card: the result waits on it (goes with the answers),
	// nothing is queued that would start a turn and clear the other ask.
	row, _ := a.chatByID(c.ID)
	if !strings.Contains(row.TurnNeeds, "migrated 3 tables") {
		t.Fatalf("result not kept on the card: %s", row.TurnNeeds)
	}
	if items, _ := a.ChatQueueList(c.ID); len(items) != 0 {
		t.Fatalf("a multi-ask card must not queue the result: %+v", items)
	}

	// The only ask: its result is the reply, queued at once.
	solo := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{{Question: "Deploy", Suggested: -1, Run: &control.TurnRun{Command: "make deploy"}}}}
	_ = a.SetTurnNeeds(c.ID, solo)
	if err := a.ChatRunAsk(c.ID, 0); err != nil {
		t.Fatal(err)
	}
	<-ran
	deadline = time.Now().Add(3 * time.Second)
	for {
		items, _ := a.ChatQueueList(c.ID)
		if len(items) == 1 && strings.Contains(items[0].Text, "migrated 3 tables") && strings.Contains(items[0].Text, "exit 0") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("solo run result not queued: %+v", items)
		}
		time.Sleep(20 * time.Millisecond)
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

func TestRunAskRefusesArchivedAndResetsInterrupted(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "t"})
	needs := control.TurnNeeds{Needs: "you", Asks: []control.TurnAsk{{Question: "Deploy?", Suggested: -1, Run: &control.TurnRun{Command: "make deploy", State: "running"}}}}
	_ = a.SetTurnNeeds(c.ID, needs)
	a.resetInterruptedRuns()
	row, _ := a.chatByID(c.ID)
	if !strings.Contains(row.TurnNeeds, `"interrupted":true`) || strings.Contains(row.TurnNeeds, `"state":"running"`) {
		t.Fatalf("a run cut by a restart must become runnable again: %s", row.TurnNeeds)
	}
	_, _ = a.db.Exec(`UPDATE chats SET archived_at = 1 WHERE id = ?`, c.ID)
	if err := a.ChatRunAsk(c.ID, 0); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("archived thread must refuse, got %v", err)
	}
}

func TestRunAskShellKillsTheWholeGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	// A grandchild holding the output pipe used to keep the read open past
	// the deadline.
	out, code := runAskShell(ctx, t.TempDir(), "sleep 30 & sleep 30")
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s: the group was not killed", time.Since(start))
	}
	if code == 0 || !strings.Contains(out, "time limit") {
		t.Fatalf("out=%q code=%d", out, code)
	}
}

func TestChatQuietArchivable(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "p"})
	child, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "c", ParentChatID: parent.ID})
	cid := strconv.FormatInt(child.ID, 10)
	a.emitChatLine(cid, chatUserKind, "the task")
	drainChatStream(t, a, cid)
	if ok, _ := a.ChatQuietArchivable(child.ID); !ok {
		t.Fatal("a child with only its task and nothing open is quiet")
	}
	a.emitChatLine(cid, chatUserKind, "wait, also check X")
	drainChatStream(t, a, cid)
	if ok, _ := a.ChatQuietArchivable(child.ID); ok {
		t.Fatal("someone wrote to it after the task: not quiet")
	}
	other, _ := a.CreateChat(Chat{WorkspaceID: wsID, Title: "c2", ParentChatID: parent.ID})
	_, _ = scheduleAdapter{a}.Add(control.ScheduledPrompt{ChatID: other.ID, Text: "later", DueAt: time.Now().Add(time.Hour)})
	if ok, _ := a.ChatQuietArchivable(other.ID); ok {
		t.Fatal("a booked check-in is a loose end: not quiet")
	}
	if ok, _ := a.ChatQuietArchivable(parent.ID); ok {
		t.Fatal("only sub-agents are quiet-archived")
	}
}
