/**
 * Provider-neutral events consumed by the embedded chat UI.
 *
 * Runtime adapters remain local child processes (Claude stream-json, Codex and
 * other ACP adapters). This mirrors T3 Code's boundary: provider protocol is
 * normalized once, while the renderer never branches on provider wire format.
 */
export type ProviderRuntimeEvent =
  | { type: "text.delta"; messageId: string; text: string }
  | { type: "tool.started"; toolCallId: string; name: string; input?: Record<string, unknown> }
  | { type: "tool.completed"; toolCallId: string; output?: string; failed?: boolean }
  | { type: "approval.requested"; requestId: string; title: string; detail?: string }
  | { type: "plan.proposed"; plan: string }
  | { type: "turn.completed"; inputTokens?: number; outputTokens?: number }
  | { type: "turn.failed"; message: string };

export function isProviderRuntimeEvent(value: unknown): value is ProviderRuntimeEvent {
  if (!value || typeof value !== "object" || !("type" in value)) return false;
  const event = value as Record<string, unknown>;
  switch (event.type) {
    case "text.delta": return typeof event.messageId === "string" && typeof event.text === "string";
    case "tool.started": return typeof event.toolCallId === "string" && typeof event.name === "string";
    case "tool.completed": return typeof event.toolCallId === "string";
    case "approval.requested": return typeof event.requestId === "string" && typeof event.title === "string";
    case "plan.proposed": return typeof event.plan === "string";
    case "turn.completed": return true;
    case "turn.failed": return typeof event.message === "string";
    default: return false;
  }
}

/** Convert Claude CLI stream-json records into renderer events. */
export function normalizeClaudeStreamEvent(value: unknown): ProviderRuntimeEvent[] {
  const event = value as Record<string, unknown>;
  if (!event || event.type !== "assistant") return [];
  const content = ((event.message as Record<string, unknown>)?.content ?? []) as Array<Record<string, unknown>>;
  const messageId = String((event.message as Record<string, unknown>)?.id ?? event.uuid ?? "claude-turn");
  return content.flatMap<ProviderRuntimeEvent>((block) => {
    if (block.type === "text" && typeof block.text === "string" && block.text) {
      return [{ type: "text.delta", messageId, text: block.text }];
    }
    if (block.type === "tool_use" && typeof block.id === "string") {
      return [{ type: "tool.started", toolCallId: block.id, name: String(block.name ?? "tool"), input: (block.input ?? {}) as Record<string, unknown> }];
    }
    return [];
  });
}

/** Convert the shared ACP parser's compact event shape into renderer events. */
export function normalizeAcpRuntimeEvent(value: unknown): ProviderRuntimeEvent[] {
  const event = value as Record<string, unknown>;
  if (!event || typeof event.kind !== "string") return [];
  switch (event.kind) {
    case "text_chunk":
      return typeof event.messageId === "string" && typeof event.text === "string"
        ? [{ type: "text.delta", messageId: `acp:${event.messageId}`, text: event.text }]
        : [];
    case "tool_call":
      return typeof event.toolCallId === "string"
        ? [{ type: "tool.started", toolCallId: event.toolCallId, name: String(event.title ?? "Tool") }]
        : [];
    case "tool_output":
      return typeof event.toolCallId === "string"
        ? [{ type: "tool.completed", toolCallId: event.toolCallId, output: typeof event.output === "string" ? event.output : "", failed: event.done !== true }]
        : [];
    default:
      return [];
  }
}
