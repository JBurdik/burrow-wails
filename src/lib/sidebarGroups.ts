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
  activityAt: (wsId: number, tab: TabSummary) => number;
  /** repo id to restrict to, or null for all */
  filterProjectId: number | null;
  /** pinned rows float to the top of their own section; optional so callers
   *  that don't care about pins (tests, future surfaces) can omit it. */
  isPinned?: (tab: TabSummary) => boolean;
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
      const row: ActivityRow = { ws, tab, ts: input.activityAt(ws.id, tab) };
      if (tab.settled) settledChats.push(row);
      else live.push(row);
    }
  }

  // Pinned first, then the existing recency order — inside the pinned block
  // and inside the rest, ordering is untouched, so unpinning restores exactly
  // the order the row would have had.
  const pinned = input.isPinned ?? (() => false);
  const order = (a: ActivityRow, b: ActivityRow) =>
    Number(pinned(b.tab)) - Number(pinned(a.tab)) || b.ts - a.ts || b.tab.id - a.tab.id;
  return { live: live.sort(order), settledChats: settledChats.sort(order) };
}

// ── project groups ───────────────────────────────────────────────────────────

/** A repo and its rows (its own tabs plus its worktrees' tabs), feed order kept. */
export interface RepoGroup {
  repoId: number;
  rows: ActivityRow[];
}

/**
 * Group the flat feed under each row's repo (a worktree folds into its parent),
 * groups ordered by their newest row, rows keeping feed order. `alwaysShow`
 * repos (the active one) get a header even with no rows.
 */
export function groupByRepo(rows: ActivityRow[], alwaysShow: number[] = []): RepoGroup[] {
  const byRepo = new Map<number, RepoGroup>();
  for (const row of rows) {
    const repoId = row.ws.parent_id ?? row.ws.id;
    let group = byRepo.get(repoId);
    if (!group) byRepo.set(repoId, (group = { repoId, rows: [] }));
    group.rows.push(row);
  }
  for (const id of alwaysShow) if (!byRepo.has(id)) byRepo.set(id, { repoId: id, rows: [] });
  return [...byRepo.values()];
}

/** `git diff --shortstat` → {added, removed}; empty output (clean tree) → zeros. */
export function parseShortstat(out: string): { added: number; removed: number } {
  const num = (re: RegExp) => Number(out.match(re)?.[1] ?? 0);
  return { added: num(/(\d+) insertions?\(\+\)/), removed: num(/(\d+) deletions?\(-\)/) };
}
