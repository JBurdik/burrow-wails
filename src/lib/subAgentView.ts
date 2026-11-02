/**
 * Pure decision for what the Right Panel's open sub-agent (`openChildId`)
 * should become after something the panel is watching changed — a re-render
 * that touched neither the active thread nor the child list, the active
 * thread itself switching, or the open child disappearing from the store
 * (deleted directly, or cascade-deleted with its parent). Kept separate from
 * RightPanel.vue so these three rules are unit-testable without a mounted
 * component or a Pinia store:
 *
 *  - nothing open stays nothing open;
 *  - an unrelated change (same thread, child still live) leaves it alone;
 *  - switching threads, or the open child no longer being among the active
 *    thread's live children, falls back to the list (`null`).
 */
export function nextSubAgentView(
  current: number | null,
  activeChatId: number | null,
  prevActiveChatId: number | null,
  liveChildIds: number[],
): number | null {
  if (current === null) return null;
  if (activeChatId !== prevActiveChatId) return null;
  if (!liveChildIds.includes(current)) return null;
  return current;
}
