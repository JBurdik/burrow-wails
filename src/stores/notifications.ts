import { defineStore } from "pinia";
import { computed, ref } from "vue";

export type NotifSource = "agent" | "git" | "system";

export interface Toast {
  id: number;
  title: string;
  body?: string;
  type: "done" | "info" | "error" | "pending";
  workspaceId?: number;
  tabId?: number;
  source?: NotifSource;
}

export interface HistoryItem extends Toast {
  ts: number;
  // absent or 1 = a single occurrence; bumped by insertHistory's dedup.
  count?: number;
}

let nextId = 0;

const STORAGE_KEY = "burrow.notifications";

function loadPersisted(): { history: HistoryItem[]; readTs: number } {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { history: [], readTs: 0 };
    const parsed = JSON.parse(raw);
    return {
      history: Array.isArray(parsed.history) ? parsed.history : [],
      readTs: typeof parsed.readTs === "number" ? parsed.readTs : 0,
    };
  } catch {
    // corrupt or unavailable localStorage must never stop the store from constructing.
    return { history: [], readTs: 0 };
  }
}

export const useNotificationsStore = defineStore("notifications", () => {
  const persisted = loadPersisted();
  // Restored rows keep last run's ids, so a counter starting at 0 would hand a
  // new toast an id already on screen — duplicate list keys, and dismiss(id)
  // reaching for the wrong row.
  for (const item of persisted.history) nextId = Math.max(nextId, item.id);
  const toasts = ref<Toast[]>([]);
  const history = ref<HistoryItem[]>(persisted.history);
  const readTs = ref(persisted.readTs);
  // pending toasts are sticky (no auto-dismiss) until resolve() gives them a
  // final type, so a long-running op's toast can't time out mid-flight.
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

  function persist() {
    try {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({ history: history.value.slice(0, 50), readTs: readTs.value }),
      );
    } catch {
      // private mode / quota exceeded — history just won't survive a restart.
    }
  }

  function isUnread(item: HistoryItem): boolean {
    return item.ts > readTs.value;
  }

  const unreadCount = computed(() => history.value.filter(isUnread).length);

  const worstUnreadType = computed<"error" | "done" | "info" | null>(() => {
    const unread = history.value.filter(isUnread);
    if (unread.some((i) => i.type === "error")) return "error";
    if (unread.some((i) => i.type === "done")) return "done";
    if (unread.some((i) => i.type === "info")) return "info";
    return null;
  });

  function arm(id: number) {
    timers.set(id, setTimeout(() => dismiss(id), 5000));
  }

  // Repeated identical notifications (same type/title/source within 60s)
  // collapse into the newest row instead of piling up — only the newest
  // row is checked, not a scan of all 50.
  function insertHistory(entry: Toast) {
    const source = entry.source ?? "system";
    const now = Date.now();
    const newest = history.value[0];
    if (
      newest &&
      newest.type === entry.type &&
      newest.title === entry.title &&
      newest.source === source &&
      now - newest.ts < 60_000
    ) {
      newest.count = (newest.count ?? 1) + 1;
      newest.ts = now;
    } else {
      history.value.unshift({ ...entry, source, ts: now });
      if (history.value.length > 50) history.value.pop();
    }
    persist();
  }

  function push(toast: Omit<Toast, "id">): number {
    const id = ++nextId;
    toasts.value.push({ ...toast, id });
    if (toast.type === "pending") return id;
    arm(id);
    insertHistory({ ...toast, id });
    return id;
  }

  // Turns a pending toast into its final state in place, so a caller never has
  // to juggle two toast ids for one operation.
  function resolve(id: number, patch: Omit<Toast, "id">) {
    const t = toasts.value.find((t) => t.id === id);
    if (!t) { push(patch); return; }
    Object.assign(t, patch);
    insertHistory(t);
    arm(id);
  }

  function dismiss(id: number) {
    const timer = timers.get(id);
    if (timer) { clearTimeout(timer); timers.delete(id); }
    const idx = toasts.value.findIndex((t) => t.id === id);
    if (idx !== -1) toasts.value.splice(idx, 1);
  }

  // ponytail: a resolve() landing the same ms as markAllRead() reads as
  // already-read. No sequence counter for this — millisecond races are fine.
  function markAllRead() {
    readTs.value = Date.now();
    persist();
  }

  function clearHistory() {
    history.value = [];
    readTs.value = Date.now();
    persist();
  }

  return {
    toasts,
    history,
    readTs,
    unreadCount,
    worstUnreadType,
    isUnread,
    push,
    resolve,
    dismiss,
    markAllRead,
    clearHistory,
  };
});
