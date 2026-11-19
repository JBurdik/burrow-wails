// Reply buttons (```action blocks, src-wails/chatactions.go). An agent writes
//   ```action
//   {"label": "Fill in the form", "kind": "new_thread" | "send", "brief": "…", "chat_id": 7}
//   ```
// and the reply shows a button there. Pressing is chat_press_action(chatId,
// msgIndex, actionIndex): Go reads the stored reply, so the index is all the
// client sends. Blocks are numbered in document order, every closed ```action
// fence counting — also one that does not parse (Go numbers it the same way).

import { escapeHtml } from "@/lib/chatBlocks";

export interface ReplyAction { label: string; kind: "new_thread" | "send"; brief: string; chat_id?: number }
export interface ActionPress {
  chat_id: number;
  msg_index: number;
  action_index: number;
  label: string;
  state: "running" | "done" | "failed";
  result_chat_id: number;
  error?: string;
}

export function parseAction(text: string): ReplyAction | null {
  let v: Record<string, unknown>;
  try {
    v = JSON.parse(text);
  } catch {
    return null;
  }
  if (!v || typeof v !== "object") return null;
  const label = typeof v.label === "string" ? v.label.trim() : "";
  const brief = typeof v.brief === "string" ? v.brief.trim() : "";
  if (!label || !brief) return null;
  if (v.kind === "new_thread") return { label, kind: "new_thread", brief };
  if (v.kind === "send" && typeof v.chat_id === "number" && v.chat_id > 0) return { label, kind: "send", brief, chat_id: v.chat_id };
  return null;
}

// ponytail: module-level render context, like chatBlocks' answeredCtx — marked
// has no per-parse state. Set by withActions around one renderChatMd call.
interface Ctx { msg: number; next: number; presses: Map<string, ActionPress>; chatTitle: (id: number) => string }
let ctx: Ctx | null = null;

export const pressKey = (msg: number, action: number) => `${msg}:${action}`;

export function withActions<T>(msg: number, presses: Map<string, ActionPress>, chatTitle: (id: number) => string, render: () => T): T {
  ctx = { msg, next: 0, presses, chatTitle };
  try {
    return render();
  } finally {
    ctx = null;
  }
}

/** One closed ```action block → its button; null = render as a code block. */
export function renderActionBlock(text: string): string | null {
  if (!ctx) return null;
  const i = ctx.next++;
  const a = parseAction(text);
  if (!a) return null;
  const p = ctx.presses.get(pressKey(ctx.msg, i));
  const state = p?.state ?? "start";
  const where = a.kind === "new_thread"
    ? "Starts a new sub-agent of this thread"
    : `Sends to ${ctx.chatTitle(a.chat_id!) || `chat #${a.chat_id}`}`;
  const tip = `${where}:\n${a.brief}${p?.state === "failed" && p.error ? `\n\nFailed: ${p.error} (press to retry)` : ""}${p?.state === "done" ? "\n\nDone — click to open it" : ""}`;
  const icon = state === "done" ? "✓" : state === "failed" ? "!" : state === "running" ? "…" : a.kind === "send" ? "↪︎" : "▶︎";
  return `<div class="ca-row"><button type="button" class="ca ca-${state}" data-ca-msg="${ctx.msg}" data-ca-index="${i}"`
    + ` data-ca-result="${p?.result_chat_id ?? 0}" data-ca-kind="${a.kind}" title="${escapeHtml(tip)}"${state === "running" ? " disabled" : ""}>`
    + `<span class="ca-icon">${icon}</span>${escapeHtml(a.label)}</button></div>`;
}
