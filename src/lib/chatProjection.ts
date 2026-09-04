import type { ChatMessage } from "@/lib/chatTypes";

// Domain events → the message list a chat renders.
//
// The wire formats are read in Go (src-wails/providerruntime.go); this is the
// second half, and the only half that still belongs to a client: it decides how
// a transcript LOOKS, which is a rendering concern.
//
// It lives here rather than inside AgentChat.vue so the rules are testable. The
// append-to-partial and tool-matching rules are where a transcript quietly goes
// wrong, and they were previously only reachable by mounting a 4000-line SFC.

/** What a projection needs to mutate. Ids come from the same counter the view uses. */
export interface ChatProjectionState {
  messages: ChatMessage[];
  nextMsgId: number;
}

/** One provider-neutral event, as emitted on `chat-event-{chatId}`. */
export interface ChatEvent {
  type: string;
  messageId?: string;
  text?: string;
  toolCallId?: string;
  name?: string;
  input?: Record<string, unknown>;
  output?: string;
  failed?: boolean;
  inputTokens?: number;
  outputTokens?: number;
  costUsd?: number;
  message?: string;
  title?: string;
  sessionId?: string;
}

export interface ChatEventBatch {
  ord: number;
  events: ChatEvent[];
}

/** Types this module renders. Everything else is for the view to act on. */
const PROJECTED = new Set(["text.delta", "thinking.delta", "user.delta", "tool.started", "tool.completed"]);

export function isProjectedEvent(type: string): boolean {
  return PROJECTED.has(type);
}

/**
 * Apply one event to the transcript. Returns true when the list changed, so the
 * caller can scroll only for events that actually moved something.
 *
 * ACP messages carry an `acp:` prefixed id and are matched BY id, because an
 * adapter interleaves several messages; native Claude deltas have no stable id
 * per bubble and are matched by position. Getting that backwards merges two
 * agents' sentences into one bubble.
 */
export function applyChatEvent(state: ChatProjectionState, event: ChatEvent): boolean {
  switch (event.type) {
    case "text.delta":
      return appendChunk(state, "assistant", event, true);
    case "thinking.delta":
      return appendChunk(state, "thinking", event, true);
    case "user.delta":
      // A replayed user turn is never "partial" — it finished long ago.
      return appendChunk(state, "user", event, false);

    case "tool.started": {
      if (!event.toolCallId) return false;
      state.messages.push({
        id: state.nextMsgId++,
        role: "tool",
        text: event.name ?? "Tool",
        toolInput: event.input ?? {},
        toolUseId: event.toolCallId,
        toolExpanded: false,
        // Native tool names are raw identifiers ("Bash") and get an icon +
        // human summary; an ACP title is already a sentence. `input` is only
        // ever set by the native transport, which is what tells them apart.
        toolRawName: Boolean(event.input),
      });
      return true;
    }

    case "tool.completed": {
      if (!event.toolCallId) return false;
      // Last matching call, not the first: a tool can be called repeatedly with
      // the same name, and only ids disambiguate them.
      const tool = [...state.messages].reverse().find((m) => m.role === "tool" && m.toolUseId === event.toolCallId);
      if (!tool) return false;
      tool.toolOutput = event.output ?? "";
      tool.toolFailed = event.failed === true;
      return true;
    }

    default:
      return false;
  }
}

function appendChunk(
  state: ChatProjectionState,
  role: "assistant" | "thinking" | "user",
  event: ChatEvent,
  partial: boolean,
): boolean {
  const text = event.text ?? "";
  if (!text) return false;
  const acpId = event.messageId?.startsWith("acp:") ? event.messageId : undefined;

  const last = acpId
    ? state.messages.filter((m) => m.role === role && m._acpMsgId === acpId && (!partial || m.partial)).pop()
    : lastOf(state.messages);

  if (last?.role === role && (last.partial || !partial)) {
    last.text += text;
    return true;
  }
  state.messages.push({
    id: state.nextMsgId++,
    role,
    text,
    ...(partial ? { partial: true } : {}),
    ...(acpId ? { _acpMsgId: acpId } : {}),
  });
  return true;
}

function lastOf(messages: ChatMessage[]): ChatMessage | undefined {
  return messages[messages.length - 1];
}

/**
 * A turn ended: nothing is still streaming. Separate from applyChatEvent
 * because the view has more to do on a turn boundary than the transcript does
 * (notifications, the queued-message flush, usage accounting).
 */
export function settleTranscript(state: ChatProjectionState): void {
  for (const m of state.messages) if (m.partial) m.partial = false;
}
