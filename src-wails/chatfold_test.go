package main

import "testing"

// Every case here is ported from src/lib/chatProjection.test.ts, plus the
// extra cases the controller called out: a tool.completed with no matching
// call is dropped (also covered on the TS side, kept here as its own case
// too), and the idempotence pair from Task 0.

func ev(t string, mut func(*ProviderRuntimeEvent)) ProviderRuntimeEvent {
	e := ProviderRuntimeEvent{Type: t}
	if mut != nil {
		mut(&e)
	}
	return e
}

func TestFoldAppendsNativeDeltasIntoOneBubbleByPosition(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "Hel" }),
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "lo" }),
	})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Text != "Hello" || !msgs[0].Partial {
		t.Fatalf("got %+v", msgs[0])
	}
}

func TestFoldStartsNewBubbleOnceSomethingCameInBetween(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "a" }),
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) {
			e.ToolCallID = "t1"
			e.Name = "Bash"
			e.Input = map[string]any{"command": "pwd"}
		}),
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "b" }),
	})
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	roles := []string{msgs[0].Role, msgs[1].Role, msgs[2].Role}
	want := []string{"assistant", "tool", "assistant"}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
	if msgs[2].Text != "b" {
		t.Fatalf("msgs[2].Text = %q, want %q", msgs[2].Text, "b")
	}
}

func TestFoldMatchesACPMessagesByIdNotPosition(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:m1"; e.Text = "one " }),
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:m2"; e.Text = "two " }),
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:m1"; e.Text = "more" }),
	})
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "one more" {
		t.Fatalf("msgs[0].Text = %q, want %q", msgs[0].Text, "one more")
	}
	if msgs[1].Text != "two " {
		t.Fatalf("msgs[1].Text = %q, want %q", msgs[1].Text, "two ")
	}
}

func TestFoldGivesEveryMessageADistinctId(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "a" }),
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Name = "Bash" }),
		ev(EvtThinkingDelta, func(e *ProviderRuntimeEvent) { e.Text = "hmm" }),
	})
	seen := map[int]bool{}
	for _, m := range msgs {
		if seen[m.ID] {
			t.Fatalf("duplicate id %d among %+v", m.ID, msgs)
		}
		seen[m.ID] = true
	}
}

func TestFoldRoutesToolResultToLastCallWithThatId(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Name = "Bash"; e.Input = map[string]any{} }),
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t2"; e.Name = "Bash"; e.Input = map[string]any{} }),
		ev(EvtToolCompleted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Output = "first" }),
		ev(EvtToolCompleted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t2"; e.Output = "boom"; e.Failed = true }),
	})
	if msgs[0].ToolUseID != "t1" || msgs[0].ToolOutput != "first" || msgs[0].ToolFailed {
		t.Fatalf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].ToolUseID != "t2" || msgs[1].ToolOutput != "boom" || !msgs[1].ToolFailed {
		t.Fatalf("msgs[1] = %+v", msgs[1])
	}
}

func TestFoldIgnoresAResultForACallItNeverSaw(t *testing.T) {
	st := &foldState{messages: []ChatMessage{}}
	changed := applyChatEvent(st, ev(EvtToolCompleted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "ghost"; e.Output = "x" }))
	if changed {
		t.Fatal("expected no change")
	}
	if len(st.messages) != 0 {
		t.Fatalf("want 0 messages, got %d", len(st.messages))
	}
}

// A tool.completed whose toolCallId matches no prior tool.started is dropped
// rather than creating a headless tool row — extra case beyond the ported
// ones, called out explicitly by the controller.
func TestFoldDropsUnmatchedToolCompletedEntirely(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Name = "Bash" }),
		ev(EvtToolCompleted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "ghost"; e.Output = "x" }),
	})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message (only the started tool), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].ToolOutput != "" {
		t.Fatalf("t1 should be untouched by a ghost completion, got %+v", msgs[0])
	}
}

func TestFoldMarksNativeToolNamesRawAndACPTitlesNot(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) {
			e.ToolCallID = "t1"
			e.Name = "Bash"
			e.Input = map[string]any{"command": "pwd"}
		}),
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t2"; e.Name = "Read file" }),
	})
	if !msgs[0].ToolRawName {
		t.Fatalf("msgs[0] should be raw, got %+v", msgs[0])
	}
	if msgs[1].ToolRawName {
		t.Fatalf("msgs[1] should not be raw, got %+v", msgs[1])
	}
}

func TestFoldRendersAReplayedUserTurnAsAFinishedBubble(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtUserDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:u1"; e.Text = "do it" }),
	})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Text != "do it" {
		t.Fatalf("got %+v", msgs[0])
	}
	if msgs[0].Partial {
		t.Fatalf("a replayed user turn must not be partial, got %+v", msgs[0])
	}
}

// Idempotence pair (Task 0): a settled id'd message delivered twice must not
// double its text; a PARTIAL message's chunks under the same id must still
// concatenate. Getting this backwards either corrupts a replayed transcript
// ("ok" -> "okok") or breaks live streaming.
func TestFoldDoesNotDoubleANonPartialMessageDeliveredTwice(t *testing.T) {
	one := ev(EvtUserDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:user:7"; e.Text = "ok" })
	msgs := foldEvents([]ProviderRuntimeEvent{one, one})
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Text != "ok" {
		t.Fatalf("msgs[0].Text = %q, want %q", msgs[0].Text, "ok")
	}
}

func TestFoldStillConcatenatesAPartialMessagesChunks(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:a1"; e.Text = "he" }),
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:a1"; e.Text = "llo" }),
	})
	if msgs[0].Text != "hello" {
		t.Fatalf("msgs[0].Text = %q, want %q", msgs[0].Text, "hello")
	}
}

func TestFoldDropsEmptyChunksInsteadOfCreatingEmptyBubbles(t *testing.T) {
	st := &foldState{messages: []ChatMessage{}}
	changed := applyChatEvent(st, ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "" }))
	if changed {
		t.Fatal("expected no change for an empty chunk")
	}
	if len(st.messages) != 0 {
		t.Fatalf("want 0 messages, got %d", len(st.messages))
	}
}

func TestFoldSettlingUnPartialsEverythingNotJustTheLastMessage(t *testing.T) {
	msgs := foldEvents([]ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "c1"; e.Text = "a" }),
		ev(EvtThinkingDelta, func(e *ProviderRuntimeEvent) { e.Text = "b" }),
	})
	settleTranscript(msgs)
	for _, m := range msgs {
		if m.Partial {
			t.Fatalf("expected every message settled, got %+v", m)
		}
	}
}

// foldEvents must be pure: folding the same events twice yields the same
// messages, with no shared/mutated state leaking between calls.
func TestFoldEventsIsPureAndDeterministic(t *testing.T) {
	events := []ProviderRuntimeEvent{
		ev(EvtTextDelta, func(e *ProviderRuntimeEvent) { e.MessageID = "acp:m1"; e.Text = "one " }),
		ev(EvtToolStarted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Name = "Bash"; e.Input = map[string]any{"x": 1} }),
		ev(EvtToolCompleted, func(e *ProviderRuntimeEvent) { e.ToolCallID = "t1"; e.Output = "done" }),
	}
	a := foldEvents(events)
	b := foldEvents(events)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		// ChatMessage contains a map (ToolInput), which is not comparable
		// with ==, so compare the fields that matter instead.
		if a[i].ID != b[i].ID || a[i].Role != b[i].Role || a[i].Text != b[i].Text {
			t.Fatalf("fold %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
