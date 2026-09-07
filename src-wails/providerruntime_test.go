package main

import (
	"reflect"
	"strings"
	"testing"

	"burrow/internal/agentphase"
)

// The first three cases mirror src/lib/providerRuntime.test.ts exactly. If this
// port ever disagrees with the TypeScript it replaced, it should fail here
// rather than in a transcript.
func TestNormalizeClaudeStreamLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []ProviderRuntimeEvent
	}{
		{
			name: "assistant text becomes a delta keyed by message id",
			line: `{"type":"assistant","message":{"id":"c1","content":[{"type":"text","text":"hi"}]}}`,
			want: []ProviderRuntimeEvent{{Type: EvtTextDelta, MessageID: "c1", Text: "hi"}},
		},
		{
			name: "tool_use becomes tool.started with its input",
			line: `{"type":"assistant","message":{"id":"c1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"pwd"}}]}}`,
			want: []ProviderRuntimeEvent{{
				Type: EvtToolStarted, ToolCallID: "t1", Name: "Bash",
				Input: map[string]any{"command": "pwd"},
			}},
		},
		{
			name: "a record with no message content yields nothing",
			line: `{"type":"assistant"}`,
			want: []ProviderRuntimeEvent{},
		},
		{
			name: "thinking blocks are concatenated into one delta, before the text",
			line: `{"type":"assistant","message":{"id":"c1","content":[{"type":"thinking","thinking":"a"},{"type":"thinking","thinking":"b"},{"type":"text","text":"out"}]}}`,
			want: []ProviderRuntimeEvent{
				{Type: EvtThinkingDelta, MessageID: "c1", Text: "ab"},
				{Type: EvtTextDelta, MessageID: "c1", Text: "out"},
			},
		},
		{
			name: "message id falls back to uuid, then to a constant",
			line: `{"type":"assistant","uuid":"u9","message":{"content":[{"type":"text","text":"x"}]}}`,
			want: []ProviderRuntimeEvent{{Type: EvtTextDelta, MessageID: "u9", Text: "x"}},
		},
		{
			name: "a tool_use with no id is dropped — nothing could match its result",
			line: `{"type":"assistant","message":{"id":"c1","content":[{"type":"tool_use","name":"Bash"}]}}`,
			want: []ProviderRuntimeEvent{},
		},
		{
			name: "tool_use with no name gets the generic one",
			line: `{"type":"assistant","message":{"id":"c1","content":[{"type":"tool_use","id":"t1"}]}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolStarted, ToolCallID: "t1", Name: "tool", Input: map[string]any{}}},
		},
		{
			// "user" here is the CLI reporting a tool's output, not a human typing.
			name: "tool_result blocks become tool.completed",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}]}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolCompleted, ToolCallID: "t1", Output: "one\ntwo"}},
		},
		{
			name: "a string tool_result body is taken as-is, and is_error marks failure",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"boom","is_error":true}]}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolCompleted, ToolCallID: "t1", Output: "boom", Failed: true}},
		},
		{
			name: "result carries the turn's usage and cost",
			line: `{"type":"result","usage":{"input_tokens":12,"output_tokens":34},"cost_usd":0.5}`,
			want: []ProviderRuntimeEvent{{Type: EvtTurnCompleted, InputTokens: 12, OutputTokens: 34, CostUSD: 0.5}},
		},
		{
			name: "a generated title rides along with the turn boundary",
			line: `{"type":"result","session_title":"Fix the parser"}`,
			want: []ProviderRuntimeEvent{
				{Type: EvtTurnCompleted},
				{Type: EvtSessionTitle, Title: "Fix the parser"},
			},
		},
		{
			name: "an errored result is a failed turn, not a completed one",
			line: `{"type":"result","subtype":"error_during_execution","result":"rate limited"}`,
			want: []ProviderRuntimeEvent{{Type: EvtTurnFailed, Message: "rate limited"}},
		},
		{
			// The native transport has always treated exit like a result.
			name: "exit is both a turn boundary and a dead process",
			line: `{"type":"exit"}`,
			want: []ProviderRuntimeEvent{{Type: EvtTurnCompleted}, {Type: EvtSessionExited}},
		},
		{
			name: "system/session_title is the title only",
			line: `{"type":"system","subtype":"session_title","title":"Ship it"}`,
			want: []ProviderRuntimeEvent{{Type: EvtSessionTitle, Title: "Ship it"}},
		},
		{
			name: "hook chatter is not transcript",
			line: `{"type":"system","subtype":"hook_started"}`,
			want: nil,
		},
		{
			name: "a permission request keeps its own channel, not this one",
			line: `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool"}}`,
			want: nil,
		},
		{
			name: "garbage is dropped, not an error — the CLI owns its own format",
			line: `not json at all`,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeClaudeStreamLine(tc.line)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeAcpLine(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []ProviderRuntimeEvent
	}{
		{
			// The acp: prefix is load-bearing — the renderer appends an ACP
			// message by id and a Claude one by position.
			name: "message chunk is prefixed so it can be matched by id",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"text":"hi"}}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtTextDelta, MessageID: "acp:m1", Text: "hi"}},
		},
		{
			name: "a chunk with no message id gets the same default as the TS",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"hi"}}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtTextDelta, MessageID: "acp:msg", Text: "hi"}},
		},
		{
			name: "thought chunk",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"hmm"}}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtThinkingDelta, Text: "hmm"}},
		},
		{
			name: "tool_call uses its title as the name",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read file"}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolStarted, ToolCallID: "t1", Name: "Read file"}},
		},
		{
			name: "a still-running tool_call_update reports nothing yet",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"in_progress"}}}`,
			want: nil,
		},
		{
			name: "completed tool_call_update collects its text blocks",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed","content":[{"content":{"type":"text","text":"out"}}]}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolCompleted, ToolCallID: "t1", Output: "out"}},
		},
		{
			name: "failed status marks the tool failed",
			line: `{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"failed"}}}`,
			want: []ProviderRuntimeEvent{{Type: EvtToolCompleted, ToolCallID: "t1", Failed: true}},
		},
		{
			name: "a JSON-RPC response is not a session update",
			line: `{"id":1,"result":{}}`,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeAcpLine(tc.line)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestToolOutputIsClipped(t *testing.T) {
	long := strings.Repeat("x", toolOutputLimit+500)
	got := NormalizeClaudeStreamLine(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"` + long + `"}]}}`)
	if len(got) != 1 || len(got[0].Output) != toolOutputLimit {
		t.Fatalf("want output clipped to %d, got %d event(s) with len %d", toolOutputLimit, len(got), len(got[0].Output))
	}
}

func TestNormalizeChatLineDispatchesOnKind(t *testing.T) {
	claude := `{"type":"assistant","message":{"id":"c1","content":[{"type":"text","text":"hi"}]}}`
	if got := NormalizeChatLine("claude-data", claude, 1); len(got) != 1 || got[0].Type != EvtTextDelta {
		t.Fatalf("claude-data not dispatched: %+v", got)
	}
	acp := `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"x"}}}}`
	if got := NormalizeChatLine("acp-data", acp, 2); len(got) != 1 || got[0].Type != EvtThinkingDelta {
		t.Fatalf("acp-data not dispatched: %+v", got)
	}
	// Permission requests are a UI decision on their own channel, never transcript.
	if got := NormalizeChatLine("acp-req", acp, 3); got != nil {
		t.Fatalf("acp-req should not normalize, got %+v", got)
	}
}

func TestUserPromptNormalizesToAUserDelta(t *testing.T) {
	// The prompt is the one stream kind this app authors rather than parses,
	// so the recorded line is the text itself.
	got := NormalizeChatLine(chatUserKind, "ship it", 7)
	if len(got) != 1 || got[0].Type != EvtUserDelta || got[0].Text != "ship it" {
		t.Fatalf("bad user event: %+v", got)
	}
}

func TestTwoIdenticalPromptsAreTwoBubbles(t *testing.T) {
	// Identified by ord, not by a hash of the text. "ok" typed twice is two
	// turns; an id derived from the text would merge them on every replay,
	// because chatProjection matches an `acp:`-prefixed id BY ID.
	first := NormalizeChatLine(chatUserKind, "ok", 4)
	second := NormalizeChatLine(chatUserKind, "ok", 9)
	if first[0].MessageID == second[0].MessageID {
		t.Fatalf("identical prompts share an id: %q", first[0].MessageID)
	}
	// ...and the SAME line replayed keeps its id, so a client that saw it live
	// recognises the replay instead of drawing the prompt twice.
	if replay := NormalizeChatLine(chatUserKind, "ok", 4); replay[0].MessageID != first[0].MessageID {
		t.Fatalf("replay changed the id: %q vs %q", replay[0].MessageID, first[0].MessageID)
	}
}

func TestAnEmptyPromptIsNotRecordedAsATurn(t *testing.T) {
	// An images-only send passes text "". Emitting an empty user bubble for it
	// would put a blank turn in both clients' transcripts.
	if got := NormalizeChatLine(chatUserKind, "", 1); got != nil {
		t.Fatalf("empty prompt produced events: %+v", got)
	}
}

func TestNormalizeAcpBurrowMarkers(t *testing.T) {
	// The handshake line is Burrow's own, and carries the id a client needs to
	// resume the session.
	got := NormalizeAcpLine(`{"_burrow":"session","sessionId":"s42","modes":null,"configOptions":[]}`)
	if len(got) != 1 || got[0].Type != EvtSessionID || got[0].SessionID != "s42" {
		t.Fatalf("session marker: %+v", got)
	}
	// Unlike the native transport: an ACP turn is settled by the response to its
	// own session/prompt, so a dead adapter must not fire a "finished" notice.
	if got := NormalizeAcpLine(`{"_burrow":"exit"}`); len(got) != 1 || got[0].Type != EvtSessionExited {
		t.Fatalf("exit marker: %+v", got)
	}
	// An unrelated JSON-RPC reply must NOT settle the turn — only the response
	// to this turn's session/prompt does, and that correlation is the sender's.
	if got := NormalizeAcpLine(`{"id":7,"result":{}}`); got != nil {
		t.Fatalf("bare response should not settle a turn: %+v", got)
	}
}

func TestChatPhaseEventMapping(t *testing.T) {
	cases := []struct {
		in   string
		want agentphase.Kind
		ok   bool
	}{
		{EvtTextDelta, agentphase.HookRunning, true},
		{EvtUserDelta, agentphase.HookRunning, true},
		// A turn that opens with a tool call reaches text much later; until it
		// did, the chat read idle through the whole thinking/tool prefix.
		{EvtThinkingDelta, agentphase.HookRunning, true},
		{EvtToolStarted, agentphase.HookRunning, true},
		{EvtTurnCompleted, agentphase.HookDone, true},
		{EvtTurnFailed, agentphase.HookError, true},
		{EvtSessionTitle, agentphase.HookSession, true},
		// The poll only walks pty: keys, so an exit is the only thing that can
		// settle a chat whose CLI died mid-turn.
		{EvtSessionExited, agentphase.Dead, true},
		{EvtToolCompleted, "", false},
		{EvtSessionID, "", false},
	}
	for _, c := range cases {
		ev, ok := chatPhaseEvent(ProviderRuntimeEvent{Type: c.in})
		if ok != c.ok {
			t.Fatalf("%q: ok=%v, want %v", c.in, ok, c.ok)
		}
		if ok && ev.Kind != c.want {
			t.Fatalf("%q → %q, want %q", c.in, ev.Kind, c.want)
		}
	}

	// A chat whose CLI dies mid-turn settles; one that exits AFTER its turn
	// completed keeps the receipt that turn earned.
	var p agentphase.Phase
	p = agentphase.Next(p, agentphase.Event{Kind: agentphase.HookRunning}, 1)
	dead, _ := chatPhaseEvent(ProviderRuntimeEvent{Type: EvtSessionExited})
	if got := agentphase.Next(p, dead, 2); got.State != agentphase.Stale {
		t.Fatalf("a chat that died mid-turn did not settle: %+v", got)
	}
	done := agentphase.Next(p, agentphase.Event{Kind: agentphase.HookDone}, 2)
	if got := agentphase.Next(done, dead, 3); got != done {
		t.Fatalf("exit after turn.completed moved the phase: %+v", got)
	}
}

func TestNormalizeAcpReplayedUserTurn(t *testing.T) {
	// session/load hands back the user's own past turns. They are transcript,
	// but only on replay — a live prompt is pushed by whoever sent it.
	got := NormalizeAcpLine(`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","messageId":"u1","content":{"text":"do it"}}}}`)
	want := []ProviderRuntimeEvent{{Type: EvtUserDelta, MessageID: "acp:u1", Text: "do it"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// --- chat-note: client-authored transcript rows/patches ---

func TestChatNoteRowNormalizesToAMessageNote(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"row","role":"system-info","text":"❓ asked a question"}`, 1)
	want := []ProviderRuntimeEvent{{Type: EvtMessageNote, Role: "system-info", Text: "❓ asked a question"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChatNoteRowCarriesImages(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"row","role":"permission","text":"granted","images":["data:x"]}`, 1)
	if len(got) != 1 || got[0].Type != EvtMessageNote || len(got[0].Images) != 1 || got[0].Images[0] != "data:x" {
		t.Fatalf("got %+v", got)
	}
}

func TestChatNotePatchNormalizesToAPatchUser(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"patch","turnMs":180000}`, 1)
	want := []ProviderRuntimeEvent{{Type: EvtMessagePatchUser, TurnMs: 180000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChatNotePatchCanCarryImagesInsteadOfTurnMs(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"patch","images":["data:x"]}`, 1)
	if len(got) != 1 || got[0].Type != EvtMessagePatchUser || got[0].TurnMs != 0 || len(got[0].Images) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestChatNoteMalformedJSONYieldsNoEvents(t *testing.T) {
	if got := NormalizeChatLine(chatNoteKind, `{not json`, 1); got != nil {
		t.Fatalf("malformed note should yield nil, got %+v", got)
	}
}

func TestChatNoteUnknownFormYieldsNoEvents(t *testing.T) {
	if got := NormalizeChatLine(chatNoteKind, `{"form":"bogus"}`, 1); got != nil {
		t.Fatalf("unknown form should yield nil, got %+v", got)
	}
}

func TestChatNoteQueuedRoleIsRejected(t *testing.T) {
	// "queued" is a transient marker that resolves inside the turn that
	// created it; persisting it would leave a dead row after a restart
	// mid-turn.
	got := NormalizeChatLine(chatNoteKind, `{"form":"row","role":"queued","text":"hang on"}`, 1)
	if got != nil {
		t.Fatalf("queued role should yield nil, got %+v", got)
	}
}

func TestChatNoteUnknownRoleIsRejected(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"row","role":"bogus","text":"x"}`, 1)
	if got != nil {
		t.Fatalf("unknown role should yield nil, got %+v", got)
	}
}

func TestChatNoteRowWithoutTextIsRejected(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"row","role":"system-info"}`, 1)
	if got != nil {
		t.Fatalf("a row with no text should yield nil, got %+v", got)
	}
}

func TestChatNotePatchWithNothingToChangeYieldsNoEvents(t *testing.T) {
	got := NormalizeChatLine(chatNoteKind, `{"form":"patch"}`, 1)
	if got != nil {
		t.Fatalf("an empty patch should yield nil, got %+v", got)
	}
}

// message.note / message.patch_user must never move the agent phase — a
// system-info marker or a permission receipt is not evidence of a running
// turn, and mistaking one for HookRunning would keep a dot orange after the
// turn that produced it already settled.
func TestChatNoteEventsDoNotMoveThePhase(t *testing.T) {
	for _, evType := range []string{EvtMessageNote, EvtMessagePatchUser} {
		if _, ok := chatPhaseEvent(ProviderRuntimeEvent{Type: evType}); ok {
			t.Fatalf("%q should not map to a phase event", evType)
		}
	}
}
