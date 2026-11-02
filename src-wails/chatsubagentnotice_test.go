package main

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

func noticeApp(t *testing.T) (*App, int64) {
	t.Helper()
	a, _, wsID, _ := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	return a, wsID
}

// step applies one runtime event to a chat the way emitChatLine does.
func step(a *App, id int64, ev ProviderRuntimeEvent) {
	key := strconv.FormatInt(id, 10)
	pev, _ := chatPhaseEvent(ev)
	before := a.phases.Get("chat:" + key)
	a.applyChatPhase(key, pev)
	a.noteSubagentPhase(key, before, a.phases.Get("chat:"+key), ev)
}

func assistantSays(t *testing.T, a *App, id int64, text string) {
	t.Helper()
	if _, err := a.db.Exec(`INSERT INTO chat_messages (chat_id, ord, payload_json) VALUES (?, 1, ?)`,
		id, `{"role":"assistant","text":`+strconv.Quote(text)+`}`); err != nil {
		t.Fatal(err)
	}
}

type sink struct {
	to   []int64
	text []string
	err  error
}

func (s *sink) send(chatID int64, text string) error {
	if s.err != nil {
		return s.err
	}
	s.to, s.text = append(s.to, chatID), append(s.text, text)
	return nil
}

// A finished sub-agent reaches its parent once, only after the parent's turn
// settles, and is marked collected.
func TestSubagentDoneDeliveredAfterParentSettlesOnce(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", Title: "Fix login", ParentChatID: parent.ID})
	assistantSays(t, a, child.ID, "all green")

	a.phases.Apply(chatKey(parent.ID), agentphase.Event{Kind: agentphase.HookRunning})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTurnCompleted})

	s := &sink{}
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 0 {
		t.Fatalf("delivered %v into a running parent turn", got)
	}
	a.phases.Apply(chatKey(parent.ID), agentphase.Event{Kind: agentphase.HookDone})
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 1 || s.to[0] != parent.ID {
		t.Fatalf("delivered %v to %v, want child once to parent", got, s.to)
	}
	want := `Sub-agent "Fix login" (#` + strconv.FormatInt(child.ID, 10) + `) finished:`
	if !strings.HasPrefix(s.text[0], want) || !strings.Contains(s.text[0], "all green") {
		t.Fatalf("text = %q", s.text[0])
	}
	if c, _ := a.chatByID(child.ID); c.CollectedAt == 0 {
		t.Fatal("child not marked collected")
	}
	if ids, _ := a.UncollectedChildren(parent.ID); len(ids) != 0 {
		t.Fatalf("collect_results would return %v again", ids)
	}

	// The same transition seen again (same TurnEndedAt) and a second sweep
	// must not deliver twice.
	ph := a.phases.Get(chatKey(child.ID))
	a.noteSubagentPhase(strconv.FormatInt(child.ID, 10), agentphase.Phase{State: agentphase.Running}, ph, ProviderRuntimeEvent{})
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 0 || len(s.text) != 1 {
		t.Fatalf("double delivery: %v %v", got, s.text)
	}
}

// A failed send is retried by the next sweep, not lost and not doubled.
func TestSubagentNoticeRetriesFailedSend(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", ParentChatID: parent.ID})
	assistantSays(t, a, child.ID, "done")
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTurnCompleted})

	s := &sink{err: errors.New("agent would not start")}
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 0 {
		t.Fatalf("delivered %v on a failed send", got)
	}
	if c, _ := a.chatByID(child.ID); c.CollectedAt != 0 {
		t.Fatal("collected on a failed send")
	}
	s.err = nil
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 1 || len(s.text) != 1 {
		t.Fatalf("retry: %v %v", got, s.text)
	}
}

// A cut-off answer stays uncollected so collect_results can return all of it.
func TestSubagentLongAnswerTruncatedAndLeftUncollected(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", ParentChatID: parent.ID})
	assistantSays(t, a, child.ID, strings.Repeat("x", subagentNoticeMaxChars+10))
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTurnCompleted})

	s := &sink{}
	a.deliverSubagentNotices(time.Now(), s.send)
	if len(s.text) != 1 || !strings.Contains(s.text[0], "collect-results") || strings.Count(s.text[0], "x") > subagentNoticeMaxChars+5 {
		t.Fatalf("text not truncated with a hint: %d chars", len(s.text))
	}
	if c, _ := a.chatByID(child.ID); c.CollectedAt != 0 {
		t.Fatal("truncated answer marked collected")
	}
}

// One blocked notice per blocked episode: a second request while still
// blocked adds nothing; blocking again after an answer is a new episode.
func TestSubagentBlockedNoticeOncePerEpisode(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", Title: "Deploy", ParentChatID: parent.ID})
	perm := ProviderRuntimeEvent{Type: EvtRequestOpened, RequestKind: RequestApproval, Name: "Bash", Input: map[string]any{"command": "rm -rf build"}}

	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, perm)
	step(a, child.ID, perm) // still blocked: same episode

	s := &sink{}
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 1 {
		t.Fatalf("delivered %v, want one blocked notice", got)
	}
	if !strings.Contains(s.text[0], "is blocked: Bash: rm -rf build") || !strings.Contains(s.text[0], "burrow chat-send --chat-id "+strconv.FormatInt(child.ID, 10)) {
		t.Fatalf("text = %q", s.text[0])
	}
	if c, _ := a.chatByID(child.ID); c.CollectedAt != 0 {
		t.Fatal("a blocked notice must not collect the child")
	}
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 0 {
		t.Fatalf("blocked notice delivered twice: %v", got)
	}

	time.Sleep(2 * time.Millisecond) // a new episode gets a new UpdatedAt
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtRequestResolved})
	step(a, child.ID, perm)
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 1 || len(s.text) != 2 {
		t.Fatalf("second episode: %v %v", got, s.text)
	}
}

// A blocked notice answered before the parent settled is dropped, not sent stale.
func TestSubagentBlockedNoticeDroppedOnceAnswered(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", ParentChatID: parent.ID})
	a.phases.Apply(chatKey(parent.ID), agentphase.Event{Kind: agentphase.HookRunning})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtTextDelta})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtRequestOpened, RequestKind: RequestInput, Name: "AskUserQuestion"})
	step(a, child.ID, ProviderRuntimeEvent{Type: EvtRequestResolved})
	a.phases.Apply(chatKey(parent.ID), agentphase.Event{Kind: agentphase.HookDone})

	s := &sink{}
	if got := a.deliverSubagentNotices(time.Now(), s.send); len(got) != 0 {
		t.Fatalf("stale blocked notice delivered: %v", s.text)
	}
}

// Side chats, top-level (detached) chats and the setting turned off record nothing.
func TestSubagentNoticeSkips(t *testing.T) {
	a, ws := noticeApp(t)
	parent, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	side, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", ParentChatID: parent.ID})
	if _, err := a.db.Exec(`UPDATE chats SET kind = 'side' WHERE id = ?`, side.ID); err != nil {
		t.Fatal(err)
	}
	top, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	child, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli", ParentChatID: parent.ID})
	writeConfig(t, map[string]any{"uiPrefs": map[string]any{"subagentAutoDeliver": false}})

	for _, id := range []int64{side.ID, top.ID, child.ID} {
		step(a, id, ProviderRuntimeEvent{Type: EvtTextDelta})
		step(a, id, ProviderRuntimeEvent{Type: EvtTurnCompleted})
	}
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM subagent_notices`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d notices recorded (err %v), want 0", n, err)
	}
}
