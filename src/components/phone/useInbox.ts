import { computed } from "vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { getAgentAttentionState } from "@/lib/terminalStatus";
import { inboxSections, type InboxRow } from "@/lib/phoneNav";

/** Every live tab of every opened workspace, as the Sidebar sees them, grouped for the Inbox. */
export function useInbox() {
  const ws = useWorkspaceStore();
  const tabs = useTerminalTabsStore();
  const sections = computed(() => {
    const rows: InboxRow[] = [];
    for (const w of ws.opened) {
      for (const t of tabs.tabsByWs[w.id] ?? []) {
        rows.push({
          wsId: w.id,
          tabId: t.id,
          title: t.title,
          state: getAgentAttentionState(t.status, tabs.isCompletionUnseen(w.id, t.id)),
          ts: tabs.activityAt(w.id, t),
          wsName: w.name,
          agentIcon: t.agentIcon,
        });
      }
    }
    return inboxSections(rows);
  });
  const needsCount = computed(() => sections.value.find((s) => s.key === "needs")?.rows.length ?? 0);
  return { sections, needsCount };
}
