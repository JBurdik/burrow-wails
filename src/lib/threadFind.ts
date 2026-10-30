// Find inside one chat transcript (⌘F). Pure: which loaded messages hold the
// query and how often, so the bar can count, step and unfold without the DOM.
// The highlight itself is painted over the rendered DOM (ThreadFindBar.vue).

import type { ChatMessage } from "@/lib/chatTypes";

export interface ThreadFindMatch {
  /** ChatMessage.id holding the match. */
  id: number;
  /** Nth occurrence inside that message's searchable text. */
  occurrence: number;
}

/** Everything find looks at in one message: its text, a tool's output and input (an ExitPlanMode plan lives there). */
export function findableText(msg: ChatMessage): string {
  if (msg.role === "queued") return "";
  if (msg.role !== "tool") return msg.text;
  const input = msg.toolInput && Object.keys(msg.toolInput).length ? JSON.stringify(msg.toolInput, null, 2) : "";
  return [msg.text, msg.toolOutput ?? "", input].join("\n");
}

/** Start offsets of every case-insensitive, non-overlapping `query` in `text`. */
export function occurrences(text: string, query: string): number[] {
  if (!query) return [];
  const hay = text.toLowerCase();
  const needle = query.toLowerCase();
  const out: number[] = [];
  for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, i + needle.length)) out.push(i);
  return out;
}

export function findMatches(messages: readonly ChatMessage[], query: string): ThreadFindMatch[] {
  if (!query) return [];
  const out: ThreadFindMatch[] = [];
  for (const msg of messages) {
    const n = occurrences(findableText(msg), query).length;
    for (let occurrence = 0; occurrence < n; occurrence++) out.push({ id: msg.id, occurrence });
  }
  return out;
}

/** Wraps both ways: next past the last is the first. */
export function stepFindIndex(index: number, total: number, delta: number): number {
  if (total <= 0) return 0;
  return (((Math.min(Math.max(index, 0), total - 1) + delta) % total) + total) % total;
}

export function formatFindCount(index: number, total: number): string {
  return total <= 0 ? "0/0" : `${Math.min(Math.max(index, 0), total - 1) + 1}/${total}`;
}
