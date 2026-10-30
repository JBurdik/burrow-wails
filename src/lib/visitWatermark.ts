import type { ChatMessage } from "@/lib/chatTypes";

// Where the "New" divider goes when a chat is reopened (t3code's
// ThreadVisitWatermark). `seenAt` is the chat's read receipt as it was BEFORE
// this visit marked it seen; a message is new when it was stamped (`at`) after
// it. Unstamped rows (older transcripts) count as already read.

/** Fewer new agent rows than this is one reply — a divider would be noise. */
const MIN_NEW_AGENT_ROWS = 2;

/** Id of the first message the user has not seen, or null for no divider. */
export function visitWatermark(messages: readonly ChatMessage[], seenAt: number | null): number | null {
  if (seenAt === null) return null;
  const first = messages.findIndex((m) => m.role !== "queued" && (m.at ?? 0) > seenAt);
  // Everything is new (a chat never opened): a divider at the top says nothing.
  if (first <= 0) return null;
  const agentRows = messages.slice(first).filter((m) => (m.role === "assistant" || m.role === "tool") && (m.at ?? 0) > seenAt);
  return agentRows.length >= MIN_NEW_AGENT_ROWS ? messages[first].id : null;
}
