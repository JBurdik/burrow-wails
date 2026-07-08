/**
 * terminalStatus.ts
 *
 * Pure helpers for terminal status: the TermStatus type, the priority ordering,
 * the aggregation used by Terminal.vue + Sidebar.vue, and tab-title derivation.
 *
 * The status TRANSITIONS live in src/machines/agentStatus.ts — this file holds no
 * state logic. Pure lib: no Vue/Tauri imports.
 */

// ── Types ─────────────────────────────────────────────────────────────────────

export type TermStatus = "idle" | "running" | "waiting" | "permission" | "done" | "review" | "error";

/** Priority high→low. Single definition consumed by Terminal.tabStatus, Sidebar.aggStatus,
 *  and FloatBubble — no more separate hard-coded priority lists.
 *  `error` is the MOST urgent: a turn that failed (StopFailure: rate_limit, overloaded,
 *  auth, billing…) outranks even a permission prompt — the user must see it first. */
export const STATUS_PRIORITY: readonly TermStatus[] = [
  "error",
  "permission",
  "waiting",
  "running",
  "review",
  "done",
  "idle",
] as const;

/** Semantic agent hook event forwarded from XTerm.vue → Terminal.vue → here. */
export type AgentEvent = "running" | "waiting" | "permission" | "done" | "error";

// ── Aggregation ───────────────────────────────────────────────────────────────

/**
 * Reduce a collection to the highest-priority status. Works for both leaves of
 * a tab (Terminal.tabStatus) and tab summaries of a workspace (Sidebar.aggStatus).
 */
export function aggregateStatus<T>(
  items: T[],
  pick: (i: T) => TermStatus,
): TermStatus {
  for (const s of STATUS_PRIORITY) {
    if (items.some((i) => pick(i) === s)) return s;
  }
  return "idle";
}

// ── Name derivation ───────────────────────────────────────────────────────────

/** True when the title is a generic auto-generated default (e.g. "Terminal 3"). */
export function isDefaultTitle(t: string): boolean {
  return /^Terminal \d+$/.test(t);
}

/**
 * Derive a consistent display name for a tab, regardless of which leaf is focused
 * or whether the tab is active/inactive.
 *
 * Priority:
 * 1. Focused leaf's title, if meaningful (non-default)
 * 2. First leaf with a meaningful title
 * 3. Focused leaf's title (even if default)
 * 4. First leaf's title
 *
 * This fixes the inconsistency where active tabs used focusedLeafId and inactive
 * tabs used getFirstLeaf, causing a split tab to show different names based on
 * which pane the user last clicked.
 */
export function deriveTabTitle(
  leaves: { title: string }[],
  focused?: { title: string },
): string {
  if (focused && !isDefaultTitle(focused.title)) return focused.title;
  const meaningful = leaves.find((l) => !isDefaultTitle(l.title));
  if (meaningful) return meaningful.title;
  return (focused ?? leaves[0])?.title ?? "";
}
