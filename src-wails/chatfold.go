package main

// Fold: provider-neutral events → the message list a chat renders.
//
// This is the second half of what `src/lib/chatProjection.ts` used to do
// alone: `providerruntime.go` already turns the wire protocol into
// ProviderRuntimeEvent; this file turns that event stream into ChatMessage
// rows, in Go, so the server can hold the transcript for every client rather
// than each client re-deriving its own copy.
//
// Ported verbatim in behaviour from chatProjection.ts's applyChatEvent +
// appendChunk + settleTranscript (including the non-idempotence fix: a
// settled message matched BY id gets its text ASSIGNED on a repeat delivery,
// not appended). See chatProjection.test.ts for the behaviour this file is
// measured against.
//
// foldEvents is PURE and deterministic: same events in, same messages out.
// The id is the index in the output slice — it was only ever a client-side
// render key, and deriving it from position (rather than carrying a
// nextMsgId counter across calls) is what makes two independent folds of the
// same event log agree. A later task adds an id OFFSET on top of this for
// callers seeding from already-stored messages; that offset does not belong
// here, since adding it to this pure function would make the id depend on
// something other than the event sequence itself.

// ChatMessage mirrors src/lib/chatTypes.ts's ChatMessage exactly: same field
// names and the same optionality (omitempty where the TS field is `?`).
// Clients render this shape unchanged, so a mismatch here silently breaks
// the transcript UI on every client at once.
type ChatMessage struct {
	ID   int    `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`

	Images  []string `json:"images,omitempty"`
	Partial bool     `json:"partial,omitempty"`

	ToolInput    map[string]any `json:"toolInput,omitempty"`
	ToolOutput   string         `json:"toolOutput,omitempty"`
	ToolUseID    string         `json:"toolUseId,omitempty"`
	ToolExpanded bool           `json:"toolExpanded,omitempty"`
	ToolFailed   bool           `json:"toolFailed,omitempty"`
	ToolRawName  bool           `json:"toolRawName,omitempty"`

	TurnMs int `json:"turnMs,omitempty"`

	// AcpMsgID is the ACP messageId — identity for incremental chunk append.
	// Unexported would drop it from JSON entirely; chatTypes.ts's `_acpMsgId`
	// is part of the persisted shape, so it has to round-trip too.
	AcpMsgID string `json:"_acpMsgId,omitempty"`
}

// foldState is the mutable accumulator threaded through folding one event
// log. It is local to a single foldEvents call — never a package variable —
// which is what keeps foldEvents pure.
type foldState struct {
	messages []ChatMessage
}

// foldEvents replays events into the message list a chat renders. Pure: same
// events in, same messages out, no clock, no id source but the sequence
// itself.
func foldEvents(events []ProviderRuntimeEvent) []ChatMessage {
	st := &foldState{messages: []ChatMessage{}}
	for _, ev := range events {
		applyChatEvent(st, ev)
	}
	return st.messages
}

// applyChatEvent applies one event to the transcript being built. Returns
// true when the list changed (unused by foldEvents today, kept because it
// mirrors the TS signature a later caller may want for incremental folding /
// scroll-on-change).
//
// ACP messages carry an `acp:`-prefixed MessageID and are matched BY id,
// because an adapter interleaves several messages at once; native Claude
// deltas have no stable id per bubble and are matched BY POSITION (the last
// message). Getting that backwards merges two agents' sentences into one
// bubble.
func applyChatEvent(st *foldState, ev ProviderRuntimeEvent) bool {
	switch ev.Type {
	case EvtTextDelta:
		return appendChunk(st, "assistant", ev, true)
	case EvtThinkingDelta:
		return appendChunk(st, "thinking", ev, true)
	case EvtUserDelta:
		// A replayed user turn is never "partial" — it finished long ago.
		return appendChunk(st, "user", ev, false)

	case EvtToolStarted:
		if ev.ToolCallID == "" {
			return false
		}
		name := ev.Name
		if name == "" {
			name = "Tool"
		}
		input := ev.Input
		if input == nil {
			input = map[string]any{}
		}
		st.messages = append(st.messages, ChatMessage{
			ID:           len(st.messages),
			Role:         "tool",
			Text:         name,
			ToolInput:    input,
			ToolUseID:    ev.ToolCallID,
			ToolExpanded: false,
			// Native tool names are raw identifiers ("Bash") and get an icon
			// + human summary; an ACP title is already a sentence. Input is
			// only ever set by the native transport, which is what tells
			// them apart.
			ToolRawName: ev.Input != nil,
		})
		return true

	case EvtToolCompleted:
		if ev.ToolCallID == "" {
			return false
		}
		// Last matching call, not the first: a tool can be called repeatedly
		// with the same name, and only ids disambiguate them.
		idx := -1
		for i := len(st.messages) - 1; i >= 0; i-- {
			if st.messages[i].Role == "tool" && st.messages[i].ToolUseID == ev.ToolCallID {
				idx = i
				break
			}
		}
		if idx == -1 {
			// A tool.completed with no matching call is dropped rather than
			// creating a headless tool row.
			return false
		}
		st.messages[idx].ToolOutput = ev.Output
		st.messages[idx].ToolFailed = ev.Failed
		return true

	default:
		return false
	}
}

// appendChunk mirrors chatProjection.ts's appendChunk. role is one of
// "assistant" | "thinking" | "user"; partial marks a still-streaming chunk.
func appendChunk(st *foldState, role string, ev ProviderRuntimeEvent, partial bool) bool {
	text := ev.Text
	if text == "" {
		return false
	}

	var acpID string
	if len(ev.MessageID) >= 4 && ev.MessageID[:4] == "acp:" {
		acpID = ev.MessageID
	}

	var last *ChatMessage
	if acpID != "" {
		for i := len(st.messages) - 1; i >= 0; i-- {
			m := &st.messages[i]
			if m.Role == role && m.AcpMsgID == acpID && (!partial || m.Partial) {
				last = m
				break
			}
		}
	} else if len(st.messages) > 0 {
		last = &st.messages[len(st.messages)-1]
	}

	if last != nil && last.Role == role && (last.Partial || !partial) {
		if acpID != "" && !partial {
			// A settled ACP message carries its whole text in one event, so
			// a repeat delivery of the same id is a duplicate, not a
			// continuation — assign, don't append, or a redelivered "ok"
			// becomes "okok".
			last.Text = text
		} else {
			last.Text += text
		}
		return true
	}

	msg := ChatMessage{
		ID:   len(st.messages),
		Role: role,
		Text: text,
	}
	if partial {
		msg.Partial = true
	}
	if acpID != "" {
		msg.AcpMsgID = acpID
	}
	st.messages = append(st.messages, msg)
	return true
}

// settleTranscript un-partials every message — a turn ended, nothing is
// still streaming. A later task calls this at turn boundaries.
func settleTranscript(messages []ChatMessage) {
	for i := range messages {
		if messages[i].Partial {
			messages[i].Partial = false
		}
	}
}
