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
  /** stable order stamp (newest first); defaults to the activity stamp */
  anchorAt?: (row: ActivityRow) => number;
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
  const at = input.anchorAt ?? ((r: ActivityRow) => r.ts);
  const order = (a: ActivityRow, b: ActivityRow) =>
    Number(pinned(b.tab)) - Number(pinned(a.tab)) || at(b) - at(a) || b.tab.id - a.tab.id;
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

export type SidebarSections = Record<SidebarSection, ActivityRow[]>;

export interface SectionInput {
  isPinned: (row: ActivityRow) => boolean;
  isSnoozed: (row: ActivityRow) => boolean;
  /** running now, or still inside the Working shelf's exit grace */
  inWorking: (row: ActivityRow) => boolean;
  /** when the row's current turn started (Working shelf order) */
  startedAt: (row: ActivityRow) => number;
}

/** Split buildActivityRows' feed into the sidebar's sections. Active keeps the
 *  feed's (anchored, stable) order: status never reorders a row — t3code's
 *  rule; attention shows on the row and its project header instead. */
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
  out.working.sort((a, b) => o.startedAt(b) - o.startedAt(a));
  return out;
}

// ── attention on rows / project headers (t3code's shouldRecedeSidebarThread,
//    resolveSidebarFolderDot) ────────────────────────────────────────────────

/** A row that needs nothing from the user is drawn quieter. `review` = an
 *  unread completion; permission / waiting / error always stay loud. */
export function rowRecedes(status: TermStatus, active: boolean): boolean {
  if (active) return false;
  return status === "idle" || status === "done" || status === "running";
}

export type GroupDot = "attention" | "failed" | "unread" | "working" | null;

/** The most urgent state among a project's rows, for the dot on its header. */
export function groupDot(statuses: TermStatus[]): GroupDot {
  if (statuses.some((s) => s === "permission" || s === "waiting")) return "attention";
  if (statuses.includes("error")) return "failed";
  if (statuses.includes("review")) return "unread";
  if (statuses.includes("running")) return "working";
  return null;
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
 * repos (the active one) get a header even with no rows. `pinned` repos always
 * get a header too and lead the list, in pin order.
 */
export function groupByRepo(rows: ActivityRow[], alwaysShow: number[] = [], pinned: number[] = [], order: number[] = []): RepoGroup[] {
  const byRepo = new Map<number, RepoGroup>();
  for (const row of rows) {
    const repoId = row.ws.parent_id ?? row.ws.id;
    let group = byRepo.get(repoId);
    if (!group) byRepo.set(repoId, (group = { repoId, rows: [] }));
    group.rows.push(row);
  }
  // Repo's own rows first, then each worktree's rows as one block (first-seen
  // order) so the Sidebar can put the worktree's name above its block.
  for (const group of byRepo.values()) {
    const first = new Map<number, number>();
    group.rows.forEach((r, i) => { if (!first.has(r.ws.id)) first.set(r.ws.id, i); });
    const key = (r: ActivityRow) => (r.ws.parent_id == null ? -1 : first.get(r.ws.id)!);
    group.rows.sort((a, b) => key(a) - key(b)); // stable: feed order within a block
  }
  for (const id of [...alwaysShow, ...pinned]) if (!byRepo.has(id)) byRepo.set(id, { repoId: id, rows: [] });
  // Pinned first, then the manual (dragged) order, then the rest in feed order.
  const rank = (id: number) =>
    pinned.includes(id) ? pinned.indexOf(id) : pinned.length + (order.includes(id) ? order.indexOf(id) : order.length);
  return [...byRepo.values()].sort((a, b) => rank(a.repoId) - rank(b.repoId)); // stable: the rest keep feed order
}

/** `git diff --shortstat` → {added, removed}; empty output (clean tree) → zeros. */
export function parseShortstat(out: string): { added: number; removed: number } {
  const num = (re: RegExp) => Number(out.match(re)?.[1] ?? 0);
  return { added: num(/(\d+) insertions?\(\+\)/), removed: num(/(\d+) deletions?\(-\)/) };
}

/**
 * A project's rows with the ones waiting on the user first (threadbear's inbox,
 * per project): `needs` is how many lead. The rest keep their order, so the
 * worktree blocks below stay intact.
 */
export function needsYouFirst<T>(rows: T[], needsYou: (row: T) => boolean): { rows: T[]; needs: number } {
  const needs = rows.filter(needsYou);
  if (!needs.length) return { rows, needs: 0 };
  return { rows: [...needs, ...rows.filter((r) => !needsYou(r))], needs: needs.length };
}

/**
 * ⌥⌘↓: the next row waiting on the user after the one on screen, wrapping;
 * from anywhere else, the first. null = nothing waits (caught up).
 */
export function nextNeedsYou<T>(rows: T[], needsYou: (row: T) => boolean, isActive: (row: T) => boolean): T | null {
  const waiting = rows.filter(needsYou);
  if (!waiting.length) return null;
  const at = rows.findIndex(isActive);
  if (at < 0) return waiting[0];
  return rows.slice(at + 1).find(needsYou) ?? (waiting[0] === rows[at] ? null : waiting[0]);
}

/**
 * The global Inbox (threadbear): rows that need you, then unread ones, lifted
 * out of their project groups (never out of `keep`, e.g. Pinned) in feed
 * order. A group left empty by the lift goes; one that was already empty
 * (the active project, a pinned one) stays.
 */
export function liftInbox<T, G extends { repoId: number; rows: T[] }>(
  groups: G[],
  needsYou: (row: T) => boolean,
  unread: (row: T) => boolean,
  keep: (repoId: number) => boolean,
): { inbox: T[]; needs: number; groups: G[] } {
  const lifted = (r: T) => needsYou(r) || unread(r);
  const pool = groups.filter((g) => !keep(g.repoId)).flatMap((g) => g.rows);
  const needs = pool.filter(needsYou);
  const inbox = [...needs, ...pool.filter((r) => !needsYou(r) && unread(r))];
  const rest = groups
    .map((g) => (keep(g.repoId) ? g : { ...g, rows: g.rows.filter((r) => !lifted(r)) }))
    .filter((g, i) => g.rows.length > 0 || groups[i].rows.length === 0);
  return { inbox, needs: needs.length, groups: rest };
}
