import { defineStore } from "pinia";
import { computed, ref } from "vue";

export interface Toast {
  id: number;
  title: string;
  body?: string;
  type: "done" | "info" | "error" | "pending";
  workspaceId?: number;
  tabId?: number;
}

export interface HistoryItem extends Toast {
  ts: number;
}

let nextId = 0;

export const useNotificationsStore = defineStore("notifications", () => {
  const toasts = ref<Toast[]>([]);
  const history = ref<HistoryItem[]>([]);
  const readTs = ref(0);
  // pending toasts are sticky (no auto-dismiss) until resolve() gives them a
  // final type, so a long-running op's toast can't time out mid-flight.
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

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

  function push(toast: Omit<Toast, "id">): number {
    const id = ++nextId;
    toasts.value.push({ ...toast, id });
    if (toast.type === "pending") return id;
    arm(id);
    history.value.unshift({ ...toast, id, ts: Date.now() });
    if (history.value.length > 50) history.value.pop();
    return id;
  }

  // Turns a pending toast into its final state in place, so a caller never has
  // to juggle two toast ids for one operation.
  function resolve(id: number, patch: Omit<Toast, "id">) {
    const t = toasts.value.find((t) => t.id === id);
    if (!t) { push(patch); return; }
    Object.assign(t, patch);
    history.value.unshift({ ...t, ts: Date.now() });
    if (history.value.length > 50) history.value.pop();
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
  }

  function clearHistory() {
    history.value = [];
    readTs.value = Date.now();
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
