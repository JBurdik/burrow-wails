import type { Workspace } from "@/stores/workspace";
import type { TabSummary } from "@/stores/terminalTabs";

// ── flat activity feed ───────────────────────────────────────────────────────

/** One tab of one workspace, as rendered in the sidebar's flat feed. */
export interface ActivityRow {
  ws: Workspace;
  tab: TabSummary;
  /** last-activity stamp, ms epoch */
  ts: number;
}

export interface ActivityInput {
  /** workspaces with a mounted Terminal — the only ones that have live tabs */
  openedWorkspaces: Workspace[];
  tabsByWs: Record<number, TabSummary[]>;
  activityAt: (wsId: number, tabId: number) => number;
  archivedIds: number[];
  /** `wsId:tabId` keys the user snoozed — see lib/snoozedTabs.ts */
  snoozedKeys?: string[];
  /** repo id to restrict to, or null for all */
  filterProjectId: number | null;
}

/**
 * Flatten every open workspace's tabs into one recency-sorted feed, splitting
 * off tabs whose workspace (or whose parent repo) the user archived, plus the
 * individual tabs they snoozed, into `snoozed`. Of what's left, a chat tab the
 * agent has finished with (see claudeChats.isSettled()) goes to `settledChats`
 * instead of `live` — distinct from `snoozed`, which is about workspace/tab
 * dormancy, not per-chat attention.
 */
export function buildActivityRows(
  input: ActivityInput,
): { live: ActivityRow[]; settledChats: ActivityRow[]; snoozed: ActivityRow[] } {
  const archived = new Set(input.archivedIds);
  const snoozedKeys = new Set(input.snoozedKeys ?? []);
  const live: ActivityRow[] = [];
  const settledChats: ActivityRow[] = [];
  const snoozed: ActivityRow[] = [];

  for (const ws of input.openedWorkspaces) {
    const repoId = ws.parent_id ?? ws.id;
    if (input.filterProjectId != null && repoId !== input.filterProjectId) continue;
    const isWsSnoozed = archived.has(ws.id) || archived.has(repoId);
    for (const tab of input.tabsByWs[ws.id] || []) {
      const row: ActivityRow = { ws, tab, ts: input.activityAt(ws.id, tab.id) };
      if (isWsSnoozed || snoozedKeys.has(`${ws.id}:${tab.id}`)) snoozed.push(row);
      else if (tab.isChat && tab.settled) settledChats.push(row);
      else live.push(row);
    }
  }

  const byRecency = (a: ActivityRow, b: ActivityRow) => b.ts - a.ts || b.tab.id - a.tab.id;
  return {
    live: live.sort(byRecency),
    settledChats: settledChats.sort(byRecency),
    snoozed: snoozed.sort(byRecency),
  };
}
