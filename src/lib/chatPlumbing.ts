import type { ChatMessage } from "@/lib/chatTypes";

// Tool calls that are plumbing, not work (as threadbear hides them): a taken
// end_turn — the card under the turn already says what it said, and its
// result is for the agent — the Stop gate's check, and ToolSearch, which only
// loads a tool. A failed one stays: the turn carried on because of it.
const PLUMBING = new Set(["mcp__burrow__end_turn", "mcp__burrow__end_turn_pending", "ToolSearch"]);

export function isPlumbingTool(m: ChatMessage): boolean {
  return m.role === "tool" && !!m.toolRawName && PLUMBING.has(m.text) && !m.toolFailed && m.toolOutput !== undefined;
}
