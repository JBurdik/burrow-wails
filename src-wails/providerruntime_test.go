package main

import (
	"reflect"
	"strings"
	"testing"
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
	if got := NormalizeChatLine("claude-data", claude); len(got) != 1 || got[0].Type != EvtTextDelta {
		t.Fatalf("claude-data not dispatched: %+v", got)
	}
	acp := `{"method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"x"}}}}`
	if got := NormalizeChatLine("acp-data", acp); len(got) != 1 || got[0].Type != EvtThinkingDelta {
		t.Fatalf("acp-data not dispatched: %+v", got)
	}
	// Permission requests are a UI decision on their own channel, never transcript.
	if got := NormalizeChatLine("acp-req", acp); got != nil {
		t.Fatalf("acp-req should not normalize, got %+v", got)
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

func TestNormalizeAcpReplayedUserTurn(t *testing.T) {
	// session/load hands back the user's own past turns. They are transcript,
	// but only on replay — a live prompt is pushed by whoever sent it.
	got := NormalizeAcpLine(`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","messageId":"u1","content":{"text":"do it"}}}}`)
	want := []ProviderRuntimeEvent{{Type: EvtUserDelta, MessageID: "acp:u1", Text: "do it"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
