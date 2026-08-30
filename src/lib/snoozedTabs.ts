import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Threads the user snoozed out of the sidebar's live feed. A snoozed row drops
// into the collapsed "Settled" section and comes back on its own the moment the
// agent changes state — so snoozing a tab you are done with for now costs
// nothing if the agent then needs you.
// ponytail: no timers, no wake-at timestamps — the agent's own status events are
// already the signal. Add a timed snooze only if waking on activity isn't enough.
const KEY = "sidebarSnoozed";

/** Snooze key for one tab of one workspace. */
export function snoozeKey(wsId: number, tabId: number): string {
  return `${wsId}:${tabId}`;
}

export const snoozedKeys = ref<string[]>([]);

configReady.then(() => {
  snoozedKeys.value = getConfig<string[]>(KEY, []);
});

export function isSnoozed(wsId: number, tabId: number): boolean {
  return snoozedKeys.value.includes(snoozeKey(wsId, tabId));
}

/** Snooze, or wake if already snoozed. */
export function toggleSnooze(wsId: number, tabId: number) {
  const key = snoozeKey(wsId, tabId);
  write(
    snoozedKeys.value.includes(key)
      ? snoozedKeys.value.filter((k) => k !== key)
      : [...snoozedKeys.value, key],
  );
}

/** Wake the given keys, if any of them are snoozed. Silent no-op otherwise. */
export function wake(keys: string[]) {
  const drop = new Set(keys);
  const next = snoozedKeys.value.filter((k) => !drop.has(k));
  if (next.length !== snoozedKeys.value.length) write(next);
}

function write(next: string[]) {
  snoozedKeys.value = next;
  setConfig(KEY, next);
}
