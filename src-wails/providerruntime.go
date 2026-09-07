package main

import (
	"encoding/json"
	"fmt"

	"burrow/internal/agentphase"
)

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

	// message.note / message.patch_user — see normalizeChatNote. Additive
	// fields only: this struct is on the wire and every existing consumer
	// (chatProjection.ts, the mobile reducer) must be unaffected by their
	// presence.
	Role   string   `json:"role,omitempty"`   // message.note: the ChatMessage role to render
	Images []string `json:"images,omitempty"` // message.note / message.patch_user
	TurnMs int      `json:"turnMs,omitempty"` // message.patch_user
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
	// message.note and message.patch_user are CLIENT-authored transcript rows
	// that no provider produces — see normalizeChatNote / chatNoteKind. They
	// carry no phase meaning (see chatPhaseEvent's fallthrough).
	EvtMessageNote      = "message.note"
	EvtMessagePatchUser = "message.patch_user"
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

// chatPhaseEvent maps a provider runtime event onto a phase event. A chat and a
// PTY carry the SAME phase type: two derivations of "is this agent busy" is how
// the mobile client's chat dots drifted from its terminal dots.
func chatPhaseEvent(ev ProviderRuntimeEvent) (agentphase.Event, bool) {
	switch ev.Type {
	// Thinking and a tool call are as much evidence of a live turn as a text
	// token — more, in fact, since a turn that opens with a tool call reaches
	// text only much later, and until then the chat read idle.
	case EvtTextDelta, EvtUserDelta, EvtThinkingDelta, EvtToolStarted:
		return agentphase.Event{Kind: agentphase.HookRunning}, true
	case EvtSessionExited:
		// The CLI is gone. Nothing else settles a chat — the foreground poll
		// only walks pty: keys — so without this a chat whose process dies
		// mid-turn stays running forever. Dead is the honest name for it, and
		// it yields `stale`: nothing failed, the process just went away. It is
		// a no-op after a turn.completed, which is the order Claude sends them.
		return agentphase.Event{Kind: agentphase.Dead}, true
	case EvtTurnCompleted:
		return agentphase.Event{Kind: agentphase.HookDone}, true
	case EvtTurnFailed:
		return agentphase.Event{Kind: agentphase.HookError, Detail: ev.Message}, true
	case EvtSessionTitle:
		return agentphase.Event{Kind: agentphase.HookSession, Title: ev.Title}, true
	}
	return agentphase.Event{}, false
}

// NormalizeChatLine dispatches on the stream kind used by chatstream.go, so a
// caller with a recorded line does not have to know which runtime produced it.
// `ord` is the line's position in chat_stream. Only the user-prompt kind uses
// it — as the bubble's identity — but it is on the signature rather than
// pushed in at one call site, because both callers (the live emit and the
// replay) already have it and a normalizer that needed it later would
// otherwise have nowhere to get it.
func NormalizeChatLine(kind, line string, ord int64) []ProviderRuntimeEvent {
	switch kind {
	case "claude-data":
		return NormalizeClaudeStreamLine(line)
	case "acp-data":
		return NormalizeAcpLine(line)
	case chatUserKind:
		return normalizeUserPrompt(line, ord)
	case chatNoteKind:
		return normalizeChatNote(line, ord)
	default:
		// acp-req is a blocking permission request — a UI decision, not
		// transcript. It keeps its own channel.
		return nil
	}
}

// chatUserKind is the stream kind for what the HUMAN sent.
//
// It needs to be its own kind rather than riding the transport's: for Claude,
// a `type:"user"` record is how the CLI reports TOOL RESULTS (see
// claudeToolResults), so a prompt published on `claude-data` would have to be
// told apart from a tool result by inspecting block types — a distinction one
// future provider tweak away from swapping a person's words for a tool's
// output. A separate kind cannot be confused with anything a provider says.
const chatUserKind = "chat-user"

// normalizeUserPrompt turns a recorded prompt into the neutral event both
// clients already know how to render.
//
// The line is the prompt text verbatim, not JSON: it is the one stream kind
// this app authors rather than parses, so there is no provider envelope to
// preserve and nothing to lose by storing what the person actually typed.
func normalizeUserPrompt(line string, ord int64) []ProviderRuntimeEvent {
	if line == "" {
		return nil
	}
	// Identified by the stream ORD, not by a hash of the text: two identical
	// prompts ("ok", "continue") are two bubbles, and hashing would merge
	// them into one on every replay. The ord is unique per line by
	// construction and is the same number on a live emit and on a replay, so
	// a client that saw the prompt live recognises the replayed copy instead
	// of drawing it twice.
	//
	// The `acp:` prefix is what makes chatProjection.ts match BY ID rather
	// than by position (see its comment on appendChunk) — the behaviour a
	// prompt needs, whichever provider is behind the chat.
	return []ProviderRuntimeEvent{{
		Type:      EvtUserDelta,
		MessageID: fmt.Sprintf("acp:user:%d", ord),
		Text:      line,
	}}
}

// chatNoteKind is the stream kind for transcript rows the CLIENT authors
// rather than something a provider said: a "question asked" / "plan ready" /
// "file edit" / "tool wants permission" system-info marker, a permission
// grant/deny receipt, or a patch onto the user bubble that opened the turn
// (attached images, elapsed turn time).
//
// Its own kind, never a transport's — same reasoning as chatUserKind: a line
// on claude-data or acp-data is something the PROVIDER said, and nothing a
// provider says should be mistaken for something the app authored (or vice
// versa). A stream reader that only knows one kind can never misfile the
// other.
const chatNoteKind = "chat-note"

// chatNoteRow and chatNotePatch are the two JSON forms recorded under
// chatNoteKind, discriminated by the "form" field. Unlike chatUserKind (plain
// text, because there is nothing to structure), a note carries a role and
// optional images/timing, which needs a real envelope:
//
//	{"form":"row",   "role":"system-info", "text":"...", "images":[...]}
//	{"form":"patch", "turnMs":180000, "images":[...]}
//
// A "row" becomes one new ChatMessage (message.note); a "patch" amends the
// last "user" bubble already in the transcript (message.patch_user) rather
// than adding a row of its own — turnMs and attached images are properties OF
// the prompt that opened the turn, not a new thing that happened.
type chatNoteEnvelope struct {
	Form   string   `json:"form"`
	Role   string   `json:"role,omitempty"`
	Text   string   `json:"text,omitempty"`
	Images []string `json:"images,omitempty"`
	TurnMs int      `json:"turnMs,omitempty"`
}

// chatNoteRoles are the ChatMessage roles (src/lib/chatTypes.ts) a "row" note
// may carry. Deliberately excludes "queued": it is a transient marker that
// resolves inside the turn that created it, so persisting one would leave a
// dead "queued" row in history after a restart mid-turn. "user"/"assistant"/
// "tool"/"thinking" are also excluded in practice — those come from the
// provider stream, not a client-authored note — but nothing stops a future
// caller from using them honestly, so the list is the ChatMessage union minus
// "queued" rather than hand-narrowed to today's two call sites
// ("system-info", "permission").
var chatNoteRoles = map[string]bool{
	"user":        true,
	"assistant":   true,
	"tool":        true,
	"thinking":    true,
	"permission":  true,
	"system-info": true,
}

// normalizeChatNote turns a recorded client-authored note into the neutral
// event(s) the fold understands. Malformed JSON, an unknown form, or a row
// with an unusable role yields NO events — a bad line must not become a
// partial or broken bubble.
func normalizeChatNote(line string, ord int64) []ProviderRuntimeEvent {
	var env chatNoteEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return nil
	}
	switch env.Form {
	case "row":
		if !chatNoteRoles[env.Role] || env.Text == "" {
			return nil
		}
		return []ProviderRuntimeEvent{{
			Type:   EvtMessageNote,
			Role:   env.Role,
			Text:   env.Text,
			Images: env.Images,
		}}
	case "patch":
		if env.TurnMs == 0 && len(env.Images) == 0 {
			// A patch that changes nothing is not an event — same rule as an
			// empty text delta: nothing downstream should render for it.
			return nil
		}
		return []ProviderRuntimeEvent{{
			Type:   EvtMessagePatchUser,
			TurnMs: env.TurnMs,
			Images: env.Images,
		}}
	default:
		return nil
	}
}
