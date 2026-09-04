package main

import "encoding/json"

// Provider protocol → provider-neutral domain events, in Go.
//
// This is the ingestion half of t3code's `ProviderRuntimeIngestion`: the wire
// format of a runtime (Claude stream-json, ACP JSON-RPC) is understood in ONE
// place, and everything downstream speaks the same vocabulary.
//
// It exists here rather than in the frontend because the parse belongs to
// whoever owns the process, not to whoever happens to be rendering. The
// frontend grew three partial copies of it — `onLine` in AgentChat.vue,
// `lib/providerRuntime.ts`, and a thinner one in `src/mobile/store.ts` for the
// remote client — and only the first is complete. A second client cannot get a
// correct transcript out of that arrangement; it has to re-implement the
// protocol to a different depth than the desktop.
//
// Ported from `src/lib/providerRuntime.ts` + `src/lib/acpParser.ts`, extended
// with the cases `onLine` still handled inline (thinking, tool_result, the
// turn boundary, the generated session title). Where the TypeScript made a
// choice, this matches it deliberately — see the tests.
//
// ponytail: a flat struct with omitempty rather than a Go interface per event
// type. The set is closed, the consumer is JSON on the other side of a Wails
// event, and a sum type here would only be pattern-matching ceremony for a
// payload that is about to be serialised anyway.

// ProviderRuntimeEvent is one thing that happened in a chat, independent of
// which agent said it.
type ProviderRuntimeEvent struct {
	Type string `json:"type"`

	// text.delta / thinking.delta
	MessageID string `json:"messageId,omitempty"`
	Text      string `json:"text,omitempty"`

	// tool.started / tool.completed
	ToolCallID string         `json:"toolCallId,omitempty"`
	Name       string         `json:"name,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Failed     bool           `json:"failed,omitempty"`

	// turn.completed
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"`

	// turn.failed / session.title / session.id
	Message   string `json:"message,omitempty"`
	Title     string `json:"title,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// Event type constants — the whole vocabulary, in one place.
const (
	EvtTextDelta     = "text.delta"
	EvtThinkingDelta = "thinking.delta"
	// A user turn replayed out of an adapter's own history (ACP session/load).
	// The live one is pushed by whoever sent it, not read off the stream.
	EvtUserDelta     = "user.delta"
	EvtToolStarted   = "tool.started"
	EvtToolCompleted = "tool.completed"
	EvtTurnCompleted = "turn.completed"
	EvtTurnFailed    = "turn.failed"
	EvtSessionTitle  = "session.title"
	EvtSessionID     = "session.id"
	// The runtime process is gone; the next send has to spawn a replacement
	// rather than write to a dead pipe. Distinct from turn.completed, which is
	// only a turn boundary and leaves the process up.
	EvtSessionExited = "session.exited"
)

// toolOutputLimit matches the frontend's slice(0, 2000): a tool result is shown
// in a collapsed card, and keeping a megabyte of build log per call in the
// transcript is what made config.json grow before it moved to SQLite.
const toolOutputLimit = 2000

func mapField(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func strField(v any) string {
	s, _ := v.(string)
	return s
}

func blocksOf(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func clip(s string) string {
	if len(s) > toolOutputLimit {
		return s[:toolOutputLimit]
	}
	return s
}

// NormalizeClaudeStreamLine turns one Claude CLI stream-json line into domain
// events. An unparseable or uninteresting line yields none — this is a
// translation, not a validation, and a record it does not know about is the
// CLI's business, not an error.
func NormalizeClaudeStreamLine(line string) []ProviderRuntimeEvent {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil
	}
	return normalizeClaudeEvent(event)
}

func normalizeClaudeEvent(event map[string]any) []ProviderRuntimeEvent {
	switch strField(event["type"]) {
	case "assistant":
		return claudeAssistant(event)
	case "user":
		return claudeToolResults(event)
	case "result":
		return claudeResult(event)
	case "exit":
		// The CLI is gone (idle-reaped by the sweeper, or it crashed). For the
		// native transport this is also a turn boundary — the frontend has
		// always treated it like a result, notification included.
		return []ProviderRuntimeEvent{{Type: EvtTurnCompleted}, {Type: EvtSessionExited}}
	case "system":
		if strField(event["subtype"]) == "session_title" {
			if t := strField(event["title"]); t != "" {
				return []ProviderRuntimeEvent{{Type: EvtSessionTitle, Title: t}}
			}
		}
		return nil
	default:
		return nil
	}
}

func claudeAssistant(event map[string]any) []ProviderRuntimeEvent {
	msg := mapField(event["message"])
	// Matches the TS: the message id, else the record's uuid, else a constant —
	// text deltas of one turn have to agree on an id to be appended together.
	messageID := strField(msg["id"])
	if messageID == "" {
		messageID = strField(event["uuid"])
	}
	if messageID == "" {
		messageID = "claude-turn"
	}

	out := []ProviderRuntimeEvent{}
	// Thinking blocks are concatenated into one delta, as the frontend did:
	// they arrive split mid-sentence and are rendered as a single bubble.
	thinking := ""
	for _, block := range blocksOf(msg["content"]) {
		if strField(block["type"]) == "thinking" {
			thinking += strField(block["thinking"])
		}
	}
	if thinking != "" {
		out = append(out, ProviderRuntimeEvent{Type: EvtThinkingDelta, MessageID: messageID, Text: thinking})
	}

	for _, block := range blocksOf(msg["content"]) {
		switch strField(block["type"]) {
		case "text":
			if text := strField(block["text"]); text != "" {
				out = append(out, ProviderRuntimeEvent{Type: EvtTextDelta, MessageID: messageID, Text: text})
			}
		case "tool_use":
			id := strField(block["id"])
			if id == "" {
				continue // nothing downstream could match its result
			}
			name := strField(block["name"])
			if name == "" {
				name = "tool"
			}
			input := mapField(block["input"])
			if input == nil {
				input = map[string]any{}
			}
			out = append(out, ProviderRuntimeEvent{Type: EvtToolStarted, ToolCallID: id, Name: name, Input: input})
		}
	}
	return out
}

// claudeToolResults: the CLI reports a tool's output as a `user` record holding
// tool_result blocks, which is why "user" here does not mean the human typed
// something.
func claudeToolResults(event map[string]any) []ProviderRuntimeEvent {
	out := []ProviderRuntimeEvent{}
	for _, block := range blocksOf(mapField(event["message"])["content"]) {
		if strField(block["type"]) != "tool_result" {
			continue
		}
		id := strField(block["tool_use_id"])
		if id == "" {
			continue
		}
		failed, _ := block["is_error"].(bool)
		out = append(out, ProviderRuntimeEvent{
			Type:       EvtToolCompleted,
			ToolCallID: id,
			Output:     clip(toolResultText(block["content"])),
			Failed:     failed,
		})
	}
	return out
}

// toolResultText accepts both shapes the CLI uses: a bare string, or content
// blocks of which only the text ones carry output.
func toolResultText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts := []string{}
	for _, block := range blocksOf(content) {
		if strField(block["type"]) == "text" {
			parts = append(parts, strField(block["text"]))
		}
	}
	return joinLines(parts)
}

func joinLines(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}

func claudeResult(event map[string]any) []ProviderRuntimeEvent {
	if subtype := strField(event["subtype"]); subtype == "error_during_execution" || subtype == "error_max_turns" {
		msg := strField(event["result"])
		if msg == "" {
			msg = subtype
		}
		return []ProviderRuntimeEvent{{Type: EvtTurnFailed, Message: msg}}
	}
	done := ProviderRuntimeEvent{Type: EvtTurnCompleted}
	if usage := mapField(event["usage"]); usage != nil {
		done.InputTokens = intOf(usage["input_tokens"])
		done.OutputTokens = intOf(usage["output_tokens"])
	}
	if cost, ok := event["cost_usd"].(float64); ok {
		done.CostUSD = cost
	}
	out := []ProviderRuntimeEvent{done}
	// Claude Code ≥1.x puts the title it generated on the result record.
	if t := strField(event["session_title"]); t != "" {
		out = append(out, ProviderRuntimeEvent{Type: EvtSessionTitle, Title: t})
	}
	return out
}

func intOf(v any) int {
	f, ok := v.(float64) // encoding/json decodes every number as float64
	if !ok {
		return 0
	}
	return int(f)
}

// NormalizeAcpLine turns one ACP `session/update` notification into domain
// events. Ported from src/lib/acpParser.ts + normalizeAcpRuntimeEvent.
func NormalizeAcpLine(line string) []ProviderRuntimeEvent {
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil
	}
	// Burrow's own markers on the ACP channel, not the adapter's: the session
	// handshake and the EOF. Both matter to any client.
	switch strField(msg["_burrow"]) {
	case "session":
		if id := strField(msg["sessionId"]); id != "" {
			return []ProviderRuntimeEvent{{Type: EvtSessionID, SessionID: id}}
		}
		return nil
	case "exit":
		// Deliberately NOT a turn boundary, unlike the native transport: an ACP
		// turn is settled by the response to its own session/prompt, and the
		// adapter dying is a separate fact. Emitting turn.completed here would
		// fire a "finished" notification for a turn nobody completed.
		return []ProviderRuntimeEvent{{Type: EvtSessionExited}}
	}

	if strField(msg["method"]) != "session/update" {
		// A JSON-RPC response also settles a turn — but only the one answering
		// the session/prompt that opened it, and that correlation lives with
		// whoever sent it (acpPromptRpcId). Guessing "any response ends the
		// turn" here would settle a turn on an unrelated reply.
		return nil
	}
	update := mapField(mapField(msg["params"])["update"])
	if update == nil {
		return nil
	}

	switch strField(update["sessionUpdate"]) {
	case "agent_message_chunk":
		id := strField(update["messageId"])
		if id == "" {
			id = "msg"
		}
		// The `acp:` prefix is load-bearing downstream: an ACP message is
		// appended by id, a Claude one by position, and the renderer tells them
		// apart by this prefix.
		return []ProviderRuntimeEvent{{
			Type:      EvtTextDelta,
			MessageID: "acp:" + id,
			Text:      strField(mapField(update["content"])["text"]),
		}}
	case "user_message_chunk":
		// Only ever seen in a session/load replay of an adapter's history.
		id := strField(update["messageId"])
		if id == "" {
			id = "u"
		}
		return []ProviderRuntimeEvent{{
			Type:      EvtUserDelta,
			MessageID: "acp:" + id,
			Text:      strField(mapField(update["content"])["text"]),
		}}
	case "agent_thought_chunk":
		return []ProviderRuntimeEvent{{
			Type: EvtThinkingDelta,
			Text: strField(mapField(update["content"])["text"]),
		}}
	case "tool_call":
		id := strField(update["toolCallId"])
		if id == "" {
			return nil
		}
		name := strField(update["title"])
		if name == "" {
			name = "Tool"
		}
		return []ProviderRuntimeEvent{{Type: EvtToolStarted, ToolCallID: id, Name: name}}
	case "tool_call_update":
		status := strField(update["status"])
		if status != "completed" && status != "failed" {
			return nil // still running — nothing to report yet
		}
		id := strField(update["toolCallId"])
		if id == "" {
			return nil
		}
		parts := []string{}
		for _, block := range blocksOf(update["content"]) {
			inner := mapField(block["content"])
			if strField(inner["type"]) == "text" {
				if t := strField(inner["text"]); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return []ProviderRuntimeEvent{{
			Type:       EvtToolCompleted,
			ToolCallID: id,
			Output:     clip(joinLines(parts)),
			Failed:     status != "completed",
		}}
	default:
		return nil
	}
}

// NormalizeChatLine dispatches on the stream kind used by chatstream.go, so a
// caller with a recorded line does not have to know which runtime produced it.
func NormalizeChatLine(kind, line string) []ProviderRuntimeEvent {
	switch kind {
	case "claude-data":
		return NormalizeClaudeStreamLine(line)
	case "acp-data":
		return NormalizeAcpLine(line)
	default:
		// acp-req is a blocking permission request — a UI decision, not
		// transcript. It keeps its own channel.
		return nil
	}
}
