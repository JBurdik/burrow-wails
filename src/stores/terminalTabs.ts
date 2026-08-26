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
  /** Kanban board task id, when this tab's first leaf was spawned by TaskDetail.vue. */
  taskId?: string;
  /** Agent-native session id (for --resume), mirrored from the leaf. */
  sessionId?: string;
}

type TabRequest = {
  wsId: number;
  action: "activate" | "add" | "close" | "reorder" | "openChat" | "rename";
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
  /** Optional Kanban board task id to stamp onto the newly-added tab's leaf (action: "add"). */
  taskId?: string;
  nonce: number;
};

export const useTerminalTabsStore = defineStore("terminalTabs", () => {
  const tabsByWs = ref<Record<number, TabSummary[]>>({});
  const activeByWs = ref<Record<number, number>>({});
  // A `review` status is terminal-owned and persists until Terminal receives
  // MARK_SEEN. Keep the sidebar responsive while that hand-off completes.
  const seenCompletionsByWs = ref<Record<number, Record<number, true>>>({});
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
    request.value = { wsId, action: "activate", tabId, nonce: ++nonce };
  }
  function add(wsId: number, cmd?: string, taskId?: string) {
    request.value = { wsId, action: "add", cmd, taskId, nonce: ++nonce };
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
  function rename(wsId: number, tabId: number, title: string) {
    request.value = { wsId, action: "rename", tabId, title, nonce: ++nonce };
  }

  return { tabsByWs, activeByWs, request, setTabs, setActive, clear, isCompletionUnseen, activate, add, close, reorder, openChat, rename };
});
