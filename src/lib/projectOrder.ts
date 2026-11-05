import { ref } from "vue";
import { followConfig, getConfig, setConfig } from "./config";

// Manual project order in the Sidebar, set by dragging a project header
// (t3code's "manual" sidebarProjectSortOrder). Repo ids, first = top. A repo
// never dragged has no entry and keeps the feed's recency order below the
// ordered ones. Pins still win (pinnedWorkspaces).
// ponytail: config key, not a column — same shape as sidebarPinned.
const KEY = "sidebarProjectOrder";

export const projectOrder = ref<number[]>([]);

followConfig([KEY], () => {
  projectOrder.value = getConfig<number[]>(KEY, []);
});

/** `shown` = the group ids as currently on screen; drop `dragId` before `targetId`. */
export function reorderProjects(shown: number[], dragId: number, targetId: number): number[] {
  if (dragId === targetId) return shown;
  const rest = shown.filter((id) => id !== dragId);
  const at = rest.indexOf(targetId);
  if (at < 0) return shown;
  rest.splice(at, 0, dragId);
  return rest;
}

export function moveProject(shown: number[], dragId: number, targetId: number) {
  const next = reorderProjects(shown, dragId, targetId);
  // Keep repos not on screen right now (no open threads) in their old relative order.
  const off = projectOrder.value.filter((id) => !next.includes(id));
  projectOrder.value = [...next, ...off];
  setConfig(KEY, projectOrder.value);
}
