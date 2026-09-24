import { ref } from "vue";
import { configReady, getConfig, setConfig } from "./config";

// Chat threads the user pinned in the sidebar feed. Pinned rows float to the top
// of whatever bucket they're already in (live / settled) — pinning is an ordering
// hint, not a section of its own.
// Keyed by the chat's SQLite id, the only stable identity a thread has: a chat
// tab's numeric tab id is re-minted from nextPtyId() on every restore (same
// reason stampKey() exists in stores/terminalTabs).
// ponytail: module-level ref like pinnedWorkspaces, not a store — one array.
const KEY = "sidebarPinnedChats";

export const pinnedChatIds = ref<number[]>([]);

configReady.then(() => {
  pinnedChatIds.value = getConfig<number[]>(KEY, []);
});

export function isChatPinned(chatId: number | undefined): boolean {
  return chatId != null && pinnedChatIds.value.includes(chatId);
}

/** Pin, or unpin if already pinned. No cap — these are the user's own threads. */
export function toggleChatPin(chatId: number) {
  const next = isChatPinned(chatId)
    ? pinnedChatIds.value.filter((x) => x !== chatId)
    : [...pinnedChatIds.value, chatId];
  pinnedChatIds.value = next;
  setConfig(KEY, next);
}

/** Drop a pin when its thread goes away — a deleted chat id must not linger. */
export function unpinChat(chatId: number) {
  if (isChatPinned(chatId)) toggleChatPin(chatId);
}
