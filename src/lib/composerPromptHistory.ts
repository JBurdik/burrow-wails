// ArrowUp / ArrowDown prompt recall in the chat composer (modelled on t3code's
// composerPromptHistory.ts). Pure: the composer keeps the position, this only
// decides what the next keystroke shows.
import type { ChatMessage } from "./chatTypes";

// Go-initiated sends that land as user rows but were never typed: sub-agent
// notices (chatsubagentnotice.go) and the usage-limit resume (chatlimit.go).
// ponytail: text match — a schedule's prompt still shows up (the user wrote it
// once); add a provenance field on ChatMessage if more senders need hiding.
const NOT_TYPED = /^(?:Sub-agent .+ \(#\d+\) |Continue where you left off\.$)/;

/** What the user typed in this thread, oldest first, consecutive repeats folded. */
export function promptHistory(messages: readonly ChatMessage[]): string[] {
  const out: string[] = [];
  for (const m of messages) {
    if (m.role !== "user") continue;
    const prompt = (m.displayText ?? m.text).trim();
    if (!prompt || NOT_TYPED.test(prompt) || out[out.length - 1] === prompt) continue;
    out.push(prompt);
  }
  return out;
}

/** Where recall stands: the entry on screen and the draft it replaced. */
export interface PromptHistoryPos { index: number; recalled: string; draft: string }

/**
 * One step through history. `pos` only counts while the composer still shows
 * what it recalled — an edit makes the text a new draft. Forward past the
 * newest entry gives the draft back. null = not ours, let the key through.
 */
export function stepPromptHistory(
  entries: readonly string[],
  pos: PromptHistoryPos | null,
  current: string,
  direction: "back" | "forward",
): { pos: PromptHistoryPos | null; text: string } | null {
  const active = pos && pos.recalled === current && entries[pos.index] === current ? pos : null;
  if (direction === "back") {
    const index = (active ? active.index : entries.length) - 1;
    if (index < 0) return null;
    return { pos: { index, recalled: entries[index], draft: active ? active.draft : current }, text: entries[index] };
  }
  if (!active) return null;
  const index = active.index + 1;
  if (index >= entries.length) return { pos: null, text: active.draft };
  return { pos: { index, recalled: entries[index], draft: active.draft }, text: entries[index] };
}

/** True when the position from stepPromptHistory is still the one on screen. */
export function recalling(pos: PromptHistoryPos | null, current: string): boolean {
  return pos !== null && pos.recalled === current;
}
