// Shapes shared by a chat's view and the session that owns its stream.
//
// These lived inside AgentChat.vue, which was fine while the component was the
// only thing that ever touched them. `lib/chatSession.ts` now holds the state
// that has to outlive the component (see docs/plans/003-view-state-routes.md,
// fáze 2′), so the types have to be reachable from outside the SFC.

export interface ChatMessage {
  id: number;
  role: "user" | "assistant" | "tool" | "thinking" | "permission" | "system-info" | "queued";
  text: string;
  images?: string[]; // data URIs for user messages with attached images
  partial?: boolean;
  toolInput?: Record<string, unknown>; // full tool args for expandable tool calls
  toolOutput?: string;  // captured tool result (first 2000 chars)
  toolUseId?: string;   // matches tool_result blocks back to tool cards
  toolExpanded?: boolean;
  toolFailed?: boolean; // tool_result came back is_error
  toolRawName?: boolean; // true when `text` is a raw tool name (native transport) vs already-human ACP title
  turnMs?: number;      // on a user message: how long the turn it opened took (persisted with the message)
  _acpMsgId?: string;   // ACP messageId — identity for incremental chunk append
}

export interface TurnStats { inputTokens: number; outputTokens: number; costUsd: number }

export interface CanUseToolReq {
  requestId: string;
  toolName: string;
  input: Record<string, unknown>;
  description?: string;
  suggestions: Array<Record<string, unknown>>;
  toolUseId?: string;
}

export interface AcpPermReq {
  rpcId: number;
  toolCallId: string;
  title: string;
  kind: string;
  options: Array<{ optionId: string; name: string; kind: string }>;
  rawInput: Record<string, unknown>;
}

export interface AcpMode { id: string; name: string; description?: string }
export interface AcpModes { currentModeId: string; availableModes: AcpMode[] }
export interface AcpConfigChoice { value: string; name: string; description?: string }
export interface AcpConfigOption {
  id: string;
  name: string;
  type: string;
  currentValue: string;
  options: AcpConfigChoice[];
}
