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
  /** repo id to restrict to, or null for all */
  filterProjectId: number | null;
}

/**
 * Flatten every open workspace's tabs into one recency-sorted feed, splitting
 * off the ones marked settled (done, no attention needed — by the user or by
 * claudeChats.isSettled()'s auto-settle). Those are the only two states a
 * *live* thread has; archiving is a per-chat thing (claudeChats.archive) and
 * after that the only step left is deletion.
 * ponytail: archiving a *project* deliberately does not touch this — it's a
 * project-list view flag. Hiding an open project's threads (incl. the active
 * one) out of the feed was the "active chat is in Snoozed" bug.
 */
export function buildActivityRows(
  input: ActivityInput,
): { live: ActivityRow[]; settledChats: ActivityRow[] } {
  const live: ActivityRow[] = [];
  const settledChats: ActivityRow[] = [];

  for (const ws of input.openedWorkspaces) {
    const repoId = ws.parent_id ?? ws.id;
    if (input.filterProjectId != null && repoId !== input.filterProjectId) continue;
    for (const tab of input.tabsByWs[ws.id] || []) {
      const row: ActivityRow = { ws, tab, ts: input.activityAt(ws.id, tab.id) };
      if (tab.settled) settledChats.push(row);
      else live.push(row);
    }
  }

  const byRecency = (a: ActivityRow, b: ActivityRow) => b.ts - a.ts || b.tab.id - a.tab.id;
  return { live: live.sort(byRecency), settledChats: settledChats.sort(byRecency) };
}
