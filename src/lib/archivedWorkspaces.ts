import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Workspaces the user archived. Purely a project-list view flag: archived repos
// sink to the bottom of the sidebar's project picker. It deliberately does NOT
// touch the thread feed — hiding an open project's threads (including the
// active one) was the "active chat is in Snoozed" bug.
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
