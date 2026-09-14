// Which sub-agent chat, if any, the Right Panel currently wants to show.
//
// A module-level ref rather than Pinia state: it is view routing for exactly
// one component (`SubAgentHost.vue`'s `<Teleport>` target), not app state
// anything else needs to read, persist or react to across a reload. The Right
// Panel (task 9) sets it; SubAgentHost only reads it to decide which mounted
// child, if any, to teleport into its `#subagent-slot`.
import { ref } from "vue";

/** Chat id of the sub-agent the panel wants on screen, or `null` when none is
 *  open. Every other mounted child stays hidden (`v-show="false"`) rather
 *  than unmounted — see SubAgentHost.vue for why. */
export const subAgentViewTarget = ref<number | null>(null);

/**
 * Pure decision for what the open child (`openChildId`/`subAgentViewTarget`)
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
