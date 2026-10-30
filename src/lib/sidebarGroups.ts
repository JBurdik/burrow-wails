import type { Workspace } from "@/stores/workspace";
import type { TabSummary } from "@/stores/terminalTabs";
import type { TermStatus } from "@/lib/terminalStatus";

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

// ── sections (t3code's Sidebar.logic resolveSidebarThreadSection) ───────────

export type SidebarSection = "snoozed" | "settled" | "pinned" | "working" | "active";

/** First match wins: snoozed > settled > pinned > working > active. A pin beats
 *  Working on purpose — a pinned thread never leaves its spot at the top. */
export function resolveSidebarSection(f: {
  snoozed: boolean;
  settled: boolean;
  pinned: boolean;
  working: boolean;
}): SidebarSection {
  if (f.snoozed) return "snoozed";
  if (f.settled) return "settled";
  if (f.pinned) return "pinned";
  if (f.working) return "working";
  return "active";
}

/** Active order: what needs the user first — permission > waiting > error >
 *  unread completion > the rest. */
export function attentionRank(status: TermStatus, unseen: boolean): number {
  if (status === "permission") return 0;
  if (status === "waiting") return 1;
  if (status === "error") return 2;
  if (status === "review" || unseen) return 3;
  return 4;
}

export type SidebarSections = Record<SidebarSection, ActivityRow[]>;

export interface SectionInput {
  isPinned: (row: ActivityRow) => boolean;
  isSnoozed: (row: ActivityRow) => boolean;
  /** running now, or still inside the Working shelf's exit grace */
  inWorking: (row: ActivityRow) => boolean;
  unseen: (row: ActivityRow) => boolean;
  /** when the row's current turn started (Working shelf order) */
  startedAt: (row: ActivityRow) => number;
}

/** Split buildActivityRows' feed (live + settled, recency-ordered) into the sidebar's sections. */
export function splitSections(rows: ActivityRow[], o: SectionInput): SidebarSections {
  const out: SidebarSections = { snoozed: [], settled: [], pinned: [], working: [], active: [] };
  for (const row of rows) {
    out[resolveSidebarSection({
      snoozed: o.isSnoozed(row),
      settled: !!row.tab.settled,
      pinned: o.isPinned(row),
      working: o.inWorking(row),
    })].push(row);
  }
  // sort() is stable, so ties keep the feed's recency order.
  out.active.sort((a, b) =>
    attentionRank(a.tab.status, o.unseen(a)) - attentionRank(b.tab.status, o.unseen(b)));
  out.working.sort((a, b) => o.startedAt(b) - o.startedAt(a));
  return out;
}

// ── Working shelf: turn start + exit grace ───────────────────────────────────

export const WORKING_EXIT_GRACE_MS = 3000;

export interface WorkingShelfState {
  /** row keys the shelf shows: running, or stopped but still lingering */
  shown: ReadonlySet<string>;
  /** key → when its current turn was first seen running */
  startedAt: ReadonlyMap<string, number>;
  /** key → when a shown row stopped running */
  stoppedAt: ReadonlyMap<string, number>;
}

export const EMPTY_WORKING_SHELF: WorkingShelfState = { shown: new Set(), startedAt: new Map(), stoppedAt: new Map() };

/**
 * One step of the shelf. A row that stops running stays WORKING_EXIT_GRACE_MS,
 * and on top of that for as long as the pointer is over the shelf (`hovering`),
 * so it never jumps out from under the cursor. `wakeAt` is when the next
 * lingering row expires — the caller steps again then.
 * ponytail: the turn start is when the client first saw `running`, not Go's
 * turn clock; a reload restamps running rows with the reload time.
 */
export function stepWorkingShelf(
  prev: WorkingShelfState,
  running: ReadonlySet<string>,
  now: number,
  hovering: boolean,
): { state: WorkingShelfState; wakeAt: number | null } {
  const shown = new Set<string>();
  const startedAt = new Map<string, number>();
  const stoppedAt = new Map<string, number>();
  let wakeAt: number | null = null;
  for (const k of running) {
    shown.add(k);
    // Running again after a stop = a new turn.
    startedAt.set(k, prev.stoppedAt.has(k) ? now : prev.startedAt.get(k) ?? now);
  }
  for (const k of prev.shown) {
    if (running.has(k)) continue;
    const t = prev.stoppedAt.get(k) ?? now;
    const until = t + WORKING_EXIT_GRACE_MS;
    if (!hovering && now >= until) continue;
    shown.add(k);
    stoppedAt.set(k, t);
    startedAt.set(k, prev.startedAt.get(k) ?? t);
    if (!hovering && now < until) wakeAt = Math.min(wakeAt ?? until, until);
  }
  return { state: { shown, startedAt, stoppedAt }, wakeAt };
}

/** The collapsed shelf still shows the open thread (t3code's routeThreadOnly). */
export function routeRowOnly(rows: ActivityRow[], isOpen: (row: ActivityRow) => boolean): ActivityRow[] {
  return rows.filter(isOpen).slice(0, 1);
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
