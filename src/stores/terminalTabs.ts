import { defineStore } from "pinia";
import { ref } from "vue";
import type { TermStatus } from "@/lib/terminalStatus";
import { snoozeKey, wake } from "@/lib/snoozedTabs";

// Lightweight mirror of each workspace's terminal tabs so the Sidebar can render
// them nested under its project. The Terminal component remains the source of
// truth (it owns the split trees / PTYs); it pushes summaries here and listens
// for activate/add/close requests coming back from the sidebar.
export interface TabSummary {
  id: number;
  title: string;
  isAgent: boolean;
  isChat?: boolean;
  busy: boolean;
  status: TermStatus;
  leafCount?: number;
  round?: number;
  /** Agent-native session id (for --resume), mirrored from the leaf. */
  sessionId?: string;
}

type TabRequest = {
  wsId: number;
  action: "activate" | "add" | "close" | "reorder" | "openChat" | "openGit" | "rename";
  tabId?: number;
  chatId?: number;
  agentId?: string;
  fromIdx?: number;
  toIdx?: number;
  title?: string;
  /** Sent as the first message right after the chat tab opens (action: "openChat"). */
  initialPrompt?: string;
  /** Optional command to run in a newly-added tab (action: "add"). */
  cmd?: string;
  nonce: number;
};

const ACTIVITY_KEY = "burrow.tab_activity";

function loadActivity(): Record<number, Record<number, number>> {
  try {
    const parsed = JSON.parse(localStorage.getItem(ACTIVITY_KEY) || "");
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

/**
 * Whether a tab's recency stamp should be reset to now. A tab we have never
 * synced before (`before` undefined) is NOT automatically new — after a restart
 * every tab looks that way, and restamping them all would flatten the order the
 * persisted stamps exist to preserve. Only a genuine change, or a tab with no
 * stamp at all, moves it.
 */
export function shouldRestamp(
  before: Pick<TabSummary, "status" | "title" | "round"> | undefined,
  tab: Pick<TabSummary, "status" | "title" | "round">,
  hasStamp: boolean,
): boolean {
  if (!hasStamp) return true;
  if (!before) return false;
  return before.status !== tab.status || before.title !== tab.title || before.round !== tab.round;
}

export const useTerminalTabsStore = defineStore("terminalTabs", () => {
  const tabsByWs = ref<Record<number, TabSummary[]>>({});
  const activeByWs = ref<Record<number, number>>({});
  // A `review` status is terminal-owned and persists until Terminal receives
  // MARK_SEEN. Keep the sidebar responsive while that hand-off completes.
  const seenCompletionsByWs = ref<Record<number, Record<number, true>>>({});
  // Last time each tab did something worth sorting on (created, status change,
  // retitled, another agent round) — deliberately NOT activation, which would
  // reshuffle the feed under the cursor. The sidebar lists tabs from every open
  // project in one flat feed, so it needs a recency key per tab.
  const activityByWs = ref<Record<number, Record<number, number>>>(loadActivity());
  const request = ref<TabRequest | null>(null);
  let nonce = 0;

  function setTabs(wsId: number, tabs: TabSummary[]) {
    const previous = new Map((tabsByWs.value[wsId] ?? []).map((tab) => [tab.id, tab.status]));
    const seen = { ...(seenCompletionsByWs.value[wsId] ?? {}) };
    const currentIds = new Set(tabs.map((tab) => tab.id));
    for (const tab of tabs) {
      // A fresh review transition is a new unseen completion, even if this tab
      // had been seen after an earlier completion.
      if (tab.status === "review" && previous.get(tab.id) !== "review") delete seen[tab.id];
      if (tab.status !== "review") delete seen[tab.id];
    }
    for (const tabId of Object.keys(seen)) {
      if (!currentIds.has(Number(tabId))) delete seen[Number(tabId)];
    }
    seenCompletionsByWs.value[wsId] = seen;

    const prevTabs = new Map((tabsByWs.value[wsId] ?? []).map((tab) => [tab.id, tab]));
    const stamps = { ...(activityByWs.value[wsId] ?? {}) };
    const now = Date.now();
    // A snoozed tab wakes on its own as soon as the agent moves — that (and the
    // tab going away) is the only thing that clears a snooze automatically.
    const toWake: string[] = [];
    for (const tab of tabs) {
      const before = prevTabs.get(tab.id);
      if (shouldRestamp(before, tab, stamps[tab.id] != null)) stamps[tab.id] = now;
      if (before && before.status !== tab.status && tab.status !== "idle") {
        toWake.push(snoozeKey(wsId, tab.id));
      }
    }
    for (const key of Object.keys(stamps)) {
      if (!currentIds.has(Number(key))) {
        toWake.push(snoozeKey(wsId, Number(key)));
        delete stamps[Number(key)];
      }
    }
    if (toWake.length) wake(toWake);
    activityByWs.value[wsId] = stamps;
    saveActivity();

    tabsByWs.value[wsId] = tabs;
  }
  function setActive(wsId: number, tabId: number) {
    activeByWs.value[wsId] = tabId;
  }
  function clear(wsId: number) {
    delete tabsByWs.value[wsId];
    delete activeByWs.value[wsId];
    delete seenCompletionsByWs.value[wsId];
  }

  function saveActivity() {
    try {
      localStorage.setItem(ACTIVITY_KEY, JSON.stringify(activityByWs.value));
    } catch {}
  }

  /** Recency key for the sidebar feed; 0 for a tab we have never seen. */
  function activityAt(wsId: number, tabId: number): number {
    return activityByWs.value[wsId]?.[tabId] ?? 0;
  }

  function isCompletionUnseen(wsId: number, tabId: number): boolean {
    return !seenCompletionsByWs.value[wsId]?.[tabId];
  }

  function markCompletionSeen(wsId: number, tabId: number) {
    seenCompletionsByWs.value[wsId] = {
      ...(seenCompletionsByWs.value[wsId] ?? {}),
      [tabId]: true,
    };
  }

  function activate(wsId: number, tabId: number) {
    // Terminal also clears its durable review state. This makes the sidebar
    // acknowledge the completion immediately, before the component round-trip.
    markCompletionSeen(wsId, tabId);
    // Deliberately does NOT stamp activity: the sidebar sorts by it, so merely
    // focusing a thread would shuffle the row out from under the cursor.
    request.value = { wsId, action: "activate", tabId, nonce: ++nonce };
  }
  function add(wsId: number, cmd?: string) {
    request.value = { wsId, action: "add", cmd, nonce: ++nonce };
  }
  function close(wsId: number, tabId: number) {
    request.value = { wsId, action: "close", tabId, nonce: ++nonce };
  }
  function reorder(wsId: number, fromIdx: number, toIdx: number) {
    request.value = { wsId, action: "reorder", fromIdx, toIdx, nonce: ++nonce };
  }
  function openChat(wsId: number, chatId?: number, agentId?: string, initialPrompt?: string) {
    request.value = { wsId, action: "openChat", chatId, agentId, initialPrompt, nonce: ++nonce };
  }
  /** Open the full git manager as a tab in that workspace's Terminal. */
  function openGit(wsId: number) {
    request.value = { wsId, action: "openGit", nonce: ++nonce };
  }
  function rename(wsId: number, tabId: number, title: string) {
    request.value = { wsId, action: "rename", tabId, title, nonce: ++nonce };
  }

  return { tabsByWs, activeByWs, activityByWs, request, setTabs, setActive, clear, activityAt, isCompletionUnseen, activate, add, close, reorder, openChat, openGit, rename };
});
