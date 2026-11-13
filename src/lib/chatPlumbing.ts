import type { ChatMessage } from "@/lib/chatTypes";

// Tool calls that are plumbing, not work (as threadbear hides them): a taken
// end_turn — the card under the turn already says what it said, and its
// result is for the agent — the Stop gate's check, and ToolSearch, which only
// loads a tool. A failed one stays: the turn carried on because of it.
const PLUMBING = new Set(["mcp__burrow__end_turn", "mcp__burrow__end_turn_pending", "ToolSearch"]);

// Codex / ACP name an MCP call "<server>: <tool>" (a human title, not a raw name).
const PLUMBING_TITLE = /^burrow[:\s]+end_turn(_pending)?$/i;

export function isPlumbingTool(m: ChatMessage): boolean {
  if (m.role !== "tool" || m.toolFailed || m.toolOutput === undefined) return false;
  return m.toolRawName ? PLUMBING.has(m.text) : PLUMBING_TITLE.test(m.text.trim());
}
