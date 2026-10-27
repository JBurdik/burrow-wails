package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"burrow/internal/agentphase"
)

func withPhases(t *testing.T, a *App) {
	t.Helper()
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
}

func userDeltas(evs []ProviderRuntimeEvent) []string {
	var out []string
	for _, ev := range evs {
		if ev.Type == EvtUserDelta {
			out = append(out, ev.Text)
		}
	}
	return out
}

func openTurnOf(a *App, id string) *turnNoteState {
	turnNotes.mu.Lock()
	defer turnNotes.mu.Unlock()
	return turnNotes.byApp[a][id]
}

func TestChatSteerClaudeWritesIntoRunningTurn(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	events := chatEvents(t, "1")

	if err := a.ChatSteer(c.ID, "early", nil); !errors.Is(err, errNoTurnToSteer) {
		t.Fatalf("no process yet: want errNoTurnToSteer, got %v", err)
	}
	if err := a.ChatSend(c.ID, "go", nil); err != nil {
		t.Fatal(err)
	}
	turn := openTurnOf(a, "1")
	if err := a.ChatSteer(c.ID, "also this", nil); err != nil {
		t.Fatalf("steer: %v", err)
	}
	fake.mu.Lock()
	last := fake.sent[len(fake.sent)-1]
	fake.mu.Unlock()
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(last), &msg) != nil || msg.Type != "user" || msg.Message.Content[0]["text"] != "also this" {
		t.Fatalf("steer must be a plain user line on stdin, got %q", last)
	}
	if got := userDeltas(events()); len(got) != 2 || got[1] != "also this" {
		t.Fatalf("steer must be recorded as the human's line: %v", got)
	}
	if openTurnOf(a, "1") != turn || turn == nil {
		t.Fatal("a steer must join the open turn, not open a new one")
	}
	if !a.phases.Get("chat:1").InFlight() {
		t.Fatal("the turn is still running")
	}

	// Blocked on the user: refused, so the approval is not painted as running.
	a.applyChatPhase("1", agentphase.Event{Kind: agentphase.HookPermission})
	if err := a.ChatSteer(c.ID, "x", nil); !errors.Is(err, errNoTurnToSteer) {
		t.Fatalf("waiting turn: want errNoTurnToSteer, got %v", err)
	}
	// Turn over: the message stays queued.
	a.applyChatPhase("1", agentphase.Event{Kind: agentphase.HookDone})
	if err := a.ChatSteer(c.ID, "late", nil); !errors.Is(err, errNoTurnToSteer) {
		t.Fatalf("ended turn: want errNoTurnToSteer, got %v", err)
	}
	if got := userDeltas(events()); len(got) != 2 {
		t.Fatalf("a refused steer must record nothing: %v", got)
	}
}

func TestChatSteerCodexSendsTurnSteer(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "codex-app-server", AgentKind: "codex"})
	events := chatEvents(t, "1")

	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	sess := &acpSession{stdin: pw, proto: protoCodexAppServer, sessionID: "thread-1", pendingTurn: 7, turnID: "turn-1"}
	a.acpReg().put("1", sess)
	t.Cleanup(func() {
		sess.mu.Lock()
		if sess.turnWatchdog != nil {
			sess.turnWatchdog.Stop()
		}
		sess.mu.Unlock()
	})
	var mu sync.Mutex
	var seen []map[string]any
	fakeCodexServer(t, a, "1", sess, pr, func(req map[string]any) map[string]any {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		p := mapOf(req["params"])
		if req["method"] == "turn/steer" && p["expectedTurnId"] == "turn-1" {
			return map[string]any{"result": map[string]any{"turnId": "turn-1"}}
		}
		return map[string]any{"error": map[string]any{"code": -32600, "message": "expected turn mismatch"}}
	})
	a.applyChatPhase("1", agentphase.Event{Kind: agentphase.HookRunning})

	if err := a.ChatSteer(c.ID, "focus on tests", []string{"data:image/png;base64,AA=="}); err != nil {
		t.Fatalf("steer: %v", err)
	}
	mu.Lock()
	req := seen[0]
	mu.Unlock()
	p := mapOf(req["params"])
	input, _ := p["input"].([]any)
	if req["method"] != "turn/steer" || p["threadId"] != "thread-1" || len(input) != 2 ||
		mapOf(input[0])["text"] != "focus on tests" || mapOf(input[1])["type"] != "image" {
		t.Fatalf("turn/steer params = %+v", req)
	}
	if got := userDeltas(events()); len(got) != 1 || got[0] != "focus on tests" {
		t.Fatalf("an accepted steer is recorded: %v", got)
	}
	sess.mu.Lock()
	pending := sess.pendingTurn
	sess.mu.Unlock()
	if pending != 7 {
		t.Fatalf("the steer reply must not settle the turn (pendingTurn=%d)", pending)
	}

	// Codex refuses (the turn moved on): error, nothing recorded.
	sess.mu.Lock()
	sess.turnID = "turn-0"
	sess.mu.Unlock()
	if err := a.ChatSteer(c.ID, "stale", nil); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("want Codex's refusal, got %v", err)
	}
	// No addressable turn yet.
	sess.mu.Lock()
	sess.turnID = ""
	sess.mu.Unlock()
	if err := a.ChatSteer(c.ID, "x", nil); !errors.Is(err, errNoTurnToSteer) {
		t.Fatalf("no turn id: want errNoTurnToSteer, got %v", err)
	}
	if got := userDeltas(events()); len(got) != 1 {
		t.Fatalf("refused steers must record nothing: %v", got)
	}
}

func TestChatSteerRefusesGenericACP(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	c, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "acp", AgentKind: "gem"})
	if err := a.ChatSteer(c.ID, "x", nil); err == nil || errors.Is(err, errNoTurnToSteer) {
		t.Fatalf("generic ACP has no steer: got %v", err)
	}
}
