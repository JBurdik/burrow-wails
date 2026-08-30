import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Workspaces the user archived out of the sidebar's main list. Repos drop into
// the collapsed "Archived" section; worktrees hide behind their repo group's
// "+N archived" toggle. Nothing is deleted — this is a view flag, so repos and
// worktrees share one id list.
// ponytail: config array, not a DB column — same shape as pinnedWorkspaces.
const KEY = "sidebarArchived";

export const archivedIds = ref<number[]>([]);

configReady.then(() => {
  archivedIds.value = getConfig<number[]>(KEY, []);
});

export function isArchived(id: number): boolean {
  return archivedIds.value.includes(id);
}

/** Archive, or unarchive if already archived. */
export function toggleArchived(id: number) {
  const next = isArchived(id)
    ? archivedIds.value.filter((x) => x !== id)
    : [...archivedIds.value, id];
  archivedIds.value = next;
  setConfig(KEY, next);
}

/** Drop an id from the archive list — call after deleting a workspace. */
export function forgetArchived(id: number) {
  if (!isArchived(id)) return;
  archivedIds.value = archivedIds.value.filter((x) => x !== id);
  setConfig(KEY, archivedIds.value);
}
