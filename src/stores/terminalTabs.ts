import { defineStore } from "pinia";
import { ref } from "vue";
import type { TermStatus } from "@/lib/terminalStatus";

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
  /** Internal claudeChats session id, for chat tabs only. */
  chatId?: number;
  /** Chat-only: no attention needed right now — see claudeChats.isSettled(). */
  settled?: boolean;
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
  /** Images paired with the first chat prompt (data URIs). */
  initialImages?: string[];
  /** Optional command to run in a newly-added tab (action: "add"). */
  cmd?: string;
  /** Working dir for a newly-added tab; defaults to the workspace's own. */
  cwd?: string;
  /** Result-capture token for a spawned sub-agent (action: "add"). */
  resultToken?: string;
  /** Add the tab without switching to it — a spawned agent shouldn't steal focus. */
  background?: boolean;
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
  // Resolvers for add() calls waiting on Terminal to report the new tab's id.
  const pendingAdds = new Map<number, (ptyId: number | undefined) => void>();

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
    for (const tab of tabs) {
      const before = prevTabs.get(tab.id);
      if (shouldRestamp(before, tab, stamps[tab.id] != null)) stamps[tab.id] = now;
    }
    for (const key of Object.keys(stamps)) {
      if (!currentIds.has(Number(key))) delete stamps[Number(key)];
    }
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
  /**
   * Ask the workspace's Terminal to open a tab. Resolves with the new tab's pty
   * id once Terminal has created it (or undefined if that workspace isn't
   * mounted to answer) — the control API hands that id back to the agent that
   * asked for the spawn, so it can supervise the tab afterwards.
   */
  function add(wsId: number, cmd?: string, extra?: { cwd?: string; resultToken?: string; background?: boolean }): Promise<number | undefined> {
    const n = ++nonce;
    request.value = { wsId, action: "add", cmd, ...extra, nonce: n };
    return new Promise((resolve) => {
      pendingAdds.set(n, resolve);
      // An unmounted workspace never answers; don't leave the caller hanging.
      setTimeout(() => {
        if (pendingAdds.delete(n)) resolve(undefined);
      }, 5000);
    });
  }

  /** Called by Terminal once a requested tab exists. */
  function fulfillAdd(reqNonce: number, ptyId: number) {
    const resolve = pendingAdds.get(reqNonce);
    if (resolve) {
      pendingAdds.delete(reqNonce);
      resolve(ptyId);
    }
  }
  function close(wsId: number, tabId: number) {
    request.value = { wsId, action: "close", tabId, nonce: ++nonce };
  }
  function reorder(wsId: number, fromIdx: number, toIdx: number) {
    request.value = { wsId, action: "reorder", fromIdx, toIdx, nonce: ++nonce };
  }
  function openChat(wsId: number, chatId?: number, agentId?: string, initialPrompt?: string, initialImages?: string[]) {
    request.value = { wsId, action: "openChat", chatId, agentId, initialPrompt, initialImages, nonce: ++nonce };
  }
  /** Open the full git manager as a tab in that workspace's Terminal. */
  function openGit(wsId: number) {
    request.value = { wsId, action: "openGit", nonce: ++nonce };
  }
  function rename(wsId: number, tabId: number, title: string) {
    request.value = { wsId, action: "rename", tabId, title, nonce: ++nonce };
  }

  return { tabsByWs, activeByWs, activityByWs, request, setTabs, setActive, clear, activityAt, isCompletionUnseen, activate, add, fulfillAdd, close, reorder, openChat, openGit, rename };
});
