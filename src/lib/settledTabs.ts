import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Manual "settled" flag for non-chat tabs (terminal/agent), keyed like
// snoozedTabs.ts. Chat tabs keep their own settle logic in claudeChats.ts
// (which also auto-settles after days of inactivity) — this module only
// covers the tabs that have no such store to carry the flag.
const KEY = "sidebarSettledTabs";

function key(wsId: number, tabId: number): string {
  return `${wsId}:${tabId}`;
}

export const settledTabKeys = ref<string[]>([]);

configReady.then(() => {
  settledTabKeys.value = getConfig<string[]>(KEY, []);
});

export function isTabSettled(wsId: number, tabId: number): boolean {
  return settledTabKeys.value.includes(key(wsId, tabId));
}

/** Settle, or mark active again if already settled. */
export function toggleTabSettled(wsId: number, tabId: number) {
  const k = key(wsId, tabId);
  write(
    settledTabKeys.value.includes(k)
      ? settledTabKeys.value.filter((existing) => existing !== k)
      : [...settledTabKeys.value, k],
  );
}

function write(next: string[]) {
  settledTabKeys.value = next;
  setConfig(KEY, next);
}
