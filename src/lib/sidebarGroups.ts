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
 * individual tabs they snoozed.
 */
export function buildActivityRows(input: ActivityInput): { live: ActivityRow[]; settled: ActivityRow[] } {
  const archived = new Set(input.archivedIds);
  const snoozed = new Set(input.snoozedKeys ?? []);
  const live: ActivityRow[] = [];
  const settled: ActivityRow[] = [];

  for (const ws of input.openedWorkspaces) {
    const repoId = ws.parent_id ?? ws.id;
    if (input.filterProjectId != null && repoId !== input.filterProjectId) continue;
    const isSettled = archived.has(ws.id) || archived.has(repoId);
    for (const tab of input.tabsByWs[ws.id] || []) {
      const bucket = isSettled || snoozed.has(`${ws.id}:${tab.id}`) ? settled : live;
      bucket.push({ ws, tab, ts: input.activityAt(ws.id, tab.id) });
    }
  }

  const byRecency = (a: ActivityRow, b: ActivityRow) => b.ts - a.ts || b.tab.id - a.tab.id;
  return { live: live.sort(byRecency), settled: settled.sort(byRecency) };
}
