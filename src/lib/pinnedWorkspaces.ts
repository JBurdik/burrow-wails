import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Workspaces the user pinned into the sidebar. The sidebar renders a tab section
// per pinned workspace (plus the active one, always), so 1–3 projects can be
// worked in without re-opening the picker.
// ponytail: module-level ref, not a store — it's one array with two callers.
const KEY = "sidebarPinned";
const MAX = 3;

export const pinnedIds = ref<number[]>([]);

configReady.then(() => {
  pinnedIds.value = getConfig<number[]>(KEY, []);
});

export function isPinned(id: number): boolean {
  return pinnedIds.value.includes(id);
}

/** Pin, or unpin if already pinned. Pinning past MAX drops the oldest pin. */
export function togglePin(id: number) {
  const next = isPinned(id)
    ? pinnedIds.value.filter((x) => x !== id)
    : [...pinnedIds.value, id].slice(-MAX);
  pinnedIds.value = next;
  setConfig(KEY, next);
}

export function unpin(id: number) {
  if (isPinned(id)) togglePin(id);
}

export { MAX as MAX_PINNED };
