import type { AgentEvent, PermissionOption } from './agentTransport'

export function parseAcpUpdate(params: unknown): AgentEvent | null {
  const p = params as Record<string, unknown>
  const u = p?.update as Record<string, unknown> | undefined
  if (!u) return null
  const disc = u.sessionUpdate as string

  switch (disc) {
    case 'agent_message_chunk': {
      const c = u.content as Record<string, unknown> | undefined
      return { kind: 'text_chunk', messageId: (u.messageId as string) ?? 'msg', text: (c?.text as string) ?? '' }
    }
    case 'agent_thought_chunk': {
      const c = u.content as Record<string, unknown> | undefined
      return { kind: 'thinking_chunk', text: (c?.text as string) ?? '' }
    }
    case 'tool_call':
      return { kind: 'tool_call', toolCallId: u.toolCallId as string, title: (u.title as string) ?? 'Tool' }
    case 'tool_call_update': {
      const status = u.status as string
      if (status !== 'completed' && status !== 'failed') return null
      const blocks = (u.content as Array<Record<string, unknown>>) ?? []
      const text = blocks
        .map(b => { const inner = b.content as Record<string, unknown> | undefined; return inner?.type === 'text' ? String(inner.text ?? '') : '' })
        .filter(Boolean).join('\n')
      return { kind: 'tool_output', toolCallId: u.toolCallId as string, output: text, done: status === 'completed' }
    }
    default:
      return null
  }
}

export function parseAcpPermRequest(raw: unknown): {
  rpcId: number; sessionId: string; toolCallId: string; options: PermissionOption[]
  title: string; kind: string; rawInput: Record<string, unknown>
} | null {
  const msg = raw as Record<string, unknown>
  const method = msg.method as string
  // Direct Codex app-server requests are translated into the same UI shape as
  // ACP permissions. The option ids are intentionally protocol-specific: Rust
  // maps them back to Codex's documented { decision } response.
  if (["item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval"].includes(method)) {
    const p = (msg.params ?? {}) as Record<string, unknown>
    const command = typeof p.command === "string" ? p.command : undefined
    const title = method.includes("commandExecution") ? (command ? `Run: ${command}` : "Run command")
      : method.includes("fileChange") ? "Apply file changes"
      : "Grant additional permission"
    return {
      rpcId: msg.id as number,
      sessionId: (p.threadId as string) ?? "",
      toolCallId: (p.itemId as string) ?? String(msg.id),
      options: [
        { optionId: "codex:accept", name: "Allow once", kind: "allow_once" },
        { optionId: "codex:acceptForSession", name: "Always allow", kind: "allow_always" },
        { optionId: "codex:decline", name: "Deny", kind: "reject_once" },
      ],
      title,
      kind: "codex-approval",
      rawInput: { ...p, ...(command ? { command } : {}) },
    }
  }
  if (method !== 'session/request_permission') return null
  const rpcId = msg.id as number
  const p = msg.params as Record<string, unknown>
  const toolCall = p?.toolCall as Record<string, unknown> | undefined
  const options = ((p?.options ?? []) as Array<Record<string, unknown>>).map(o => ({
    optionId: o.optionId as string, name: o.name as string, kind: o.kind as string
  }))
  return {
    rpcId,
    sessionId: p?.sessionId as string,
    toolCallId: toolCall?.toolCallId as string,
    options,
    title: (toolCall?.title as string) ?? 'Tool',
    kind: (toolCall?.kind as string) ?? 'other',
    rawInput: (toolCall?.rawInput as Record<string, unknown>) ?? {},
  }
}
