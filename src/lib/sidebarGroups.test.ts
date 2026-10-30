import { describe, it, expect } from "vitest";
import {
  buildActivityRows, groupByRepo, parseShortstat, resolveSidebarSection, splitSections, stepWorkingShelf,
  routeRowOnly, EMPTY_WORKING_SHELF, WORKING_EXIT_GRACE_MS, type ActivityRow, type SectionInput,
} from "./sidebarGroups";
import type { Workspace } from "@/stores/workspace";
import type { TabSummary } from "@/stores/terminalTabs";

function mkWs(id: number, name: string, parent_id: number | null = null): Workspace {
  return { id, name, path: `/r/${name}`, created_at: 0, last_opened: null, parent_id, sort_order: id };
}
function mkTab(id: number, title: string, extra?: Partial<TabSummary>): TabSummary {
  return { id, title, isAgent: false, busy: false, status: "idle", ...extra };
}

const repoA = mkWs(1, "a");
const repoB = mkWs(2, "b");
const wtA1 = mkWs(10, "feat", 1);

describe("buildActivityRows", () => {
  const tabsByWs = { 1: [mkTab(100, "a1"), mkTab(101, "a2")], 10: [mkTab(200, "wt")], 2: [mkTab(300, "b1")] };
  const stamps: Record<number, number> = { 100: 50, 101: 90, 200: 70, 300: 10 };
  const activityAt = (_ws: number, tab: { id: number }) => stamps[tab.id] ?? 0;
  const open = [repoA, wtA1, repoB];

  it("flattens every open workspace's tabs, newest first", () => {
    const { live, settledChats } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, filterProjectId: null });
    expect(live.map((r) => r.tab.id)).toEqual([101, 200, 100, 300]);
    expect(settledChats).toEqual([]);
  });

  it("filters by repo, keeping that repo's worktree rows", () => {
    const { live } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, filterProjectId: 1 });
    expect(live.map((r) => r.tab.id)).toEqual([101, 200, 100]);
  });

  it("keeps never-stamped tabs in a stable order", () => {
    const { live } = buildActivityRows({
      openedWorkspaces: [repoB],
      tabsByWs: { 2: [mkTab(300, "b1"), mkTab(301, "b2")] },
      activityAt: () => 0, filterProjectId: null,
    });
    expect(live.map((r) => r.tab.id)).toEqual([301, 300]);
  });

  it("buckets a settled chat tab separately from live", () => {
    const { live, settledChats } = buildActivityRows({
      openedWorkspaces: [repoA],
      tabsByWs: { 1: [mkTab(100, "a1"), mkTab(101, "a2", { isChat: true, settled: true })] },
      activityAt, filterProjectId: null,
    });
    expect(live.map((r) => r.tab.id)).toEqual([100]);
    expect(settledChats.map((r) => r.tab.id)).toEqual([101]);
  });
});

describe("buildActivityRows pinning", () => {
  const tabs = [
    mkTab(100, "a1", { isChat: true, chatId: 1 }),
    mkTab(101, "a2", { isChat: true, chatId: 2 }),
    mkTab(102, "a3", { isChat: true, chatId: 3 }),
  ];
  const stamps: Record<number, number> = { 100: 30, 101: 90, 102: 60 };
  const run = (pins: number[]) =>
    buildActivityRows({
      openedWorkspaces: [repoA],
      tabsByWs: { 1: tabs },
      activityAt: (_ws, tab) => stamps[tab.id] ?? 0,
      filterProjectId: null,
      isPinned: (tab) => tab.chatId != null && pins.includes(tab.chatId),
    });

  it("floats pinned rows above the recency order", () => {
    expect(run([1]).live.map((r) => r.tab.id)).toEqual([100, 101, 102]);
  });

  it("keeps recency order among pinned rows and among the rest", () => {
    expect(run([1, 3]).live.map((r) => r.tab.id)).toEqual([102, 100, 101]);
  });

  it("restores the plain recency order once unpinned", () => {
    expect(run([]).live.map((r) => r.tab.id)).toEqual([101, 102, 100]);
  });

  it("pins inside the settled bucket, not out of it", () => {
    const { live, settledChats } = buildActivityRows({
      openedWorkspaces: [repoA],
      tabsByWs: { 1: [tabs[0], { ...tabs[1], settled: true }, { ...tabs[2], settled: true }] },
      activityAt: (_ws, tab) => stamps[tab.id] ?? 0,
      filterProjectId: null,
      isPinned: (tab) => tab.chatId === 3,
    });
    expect(live.map((r) => r.tab.id)).toEqual([100]);
    expect(settledChats.map((r) => r.tab.id)).toEqual([102, 101]);
  });
});

describe("groupByRepo", () => {
  const row = (wsId: number, parent: number | null, tabId: string) =>
    ({ ws: { id: wsId, parent_id: parent }, tab: { id: tabId }, ts: 0 }) as unknown as ActivityRow;
  it("folds worktrees into their repo, keeps feed order, adds empty always-shown repos", () => {
    const groups = groupByRepo([row(1, null, "a"), row(5, 2, "b"), row(1, null, "c"), row(2, null, "d")], [2, 9]);
    expect(groups.map((g) => [g.repoId, g.rows.map((r) => r.tab.id)])).toEqual([[1, ["a", "c"]], [2, ["b", "d"]], [9, []]]);
  });
});

describe("parseShortstat", () => {
  it("reads insertions and deletions, either may be missing", () => {
    expect(parseShortstat(" 3 files changed, 142 insertions(+), 37 deletions(-)\n")).toEqual({ added: 142, removed: 37 });
    expect(parseShortstat(" 1 file changed, 1 insertion(+)")).toEqual({ added: 1, removed: 0 });
    expect(parseShortstat("")).toEqual({ added: 0, removed: 0 });
  });
});

describe("resolveSidebarSection", () => {
  const f = { snoozed: false, settled: false, pinned: false, working: false };
  it("applies snoozed > settled > pinned > working > active", () => {
    expect(resolveSidebarSection({ snoozed: true, settled: true, pinned: true, working: true })).toBe("snoozed");
    expect(resolveSidebarSection({ ...f, settled: true, pinned: true, working: true })).toBe("settled");
    expect(resolveSidebarSection({ ...f, pinned: true, working: true })).toBe("pinned");
    expect(resolveSidebarSection({ ...f, working: true })).toBe("working");
    expect(resolveSidebarSection(f)).toBe("active");
  });
});

describe("splitSections", () => {
  const row = (id: number, ts: number, extra?: Partial<TabSummary>): ActivityRow => ({ ws: repoA, tab: mkTab(id, `t${id}`, extra), ts });
  const base: SectionInput = {
    isPinned: () => false, isSnoozed: () => false, unseen: () => false,
    inWorking: (r) => r.tab.status === "running", startedAt: () => 0,
  };

  it("puts a running PTY tab and a running chat into Working, newest turn first", () => {
    const pty = row(1, 10, { isAgent: true, status: "running" });
    const chat = row(2, 20, { isChat: true, chatId: 7, status: "running" });
    const started: Record<number, number> = { 1: 500, 2: 100 };
    const s = splitSections([chat, pty], { ...base, startedAt: (r) => started[r.tab.id] });
    expect(s.working.map((r) => r.tab.id)).toEqual([1, 2]);
    expect(s.active).toEqual([]);
  });

  it("keeps a pinned running row in Pinned and a snoozed one in Snoozed", () => {
    const pinned = row(1, 10, { isChat: true, chatId: 1, status: "running" });
    const snoozed = row(2, 20, { isChat: true, chatId: 2, settled: true });
    const s = splitSections([pinned, snoozed], { ...base, isPinned: (r) => r.tab.id === 1, isSnoozed: (r) => r.tab.id === 2 });
    expect(s.pinned.map((r) => r.tab.id)).toEqual([1]);
    expect(s.snoozed.map((r) => r.tab.id)).toEqual([2]);
    expect(s.working).toEqual([]);
    expect(s.settled).toEqual([]);
  });

  it("orders Active by attention, then recency", () => {
    const rows = [
      row(1, 90),
      row(2, 80, { status: "review" }),
      row(3, 70, { status: "waiting" }),
      row(4, 60, { status: "permission" }),
      row(5, 50, { status: "done" }),
      row(6, 40),
    ];
    const s = splitSections(rows, { ...base, unseen: (r) => r.tab.id === 5 });
    expect(s.active.map((r) => r.tab.id)).toEqual([4, 3, 2, 5, 1, 6]);
  });
});

describe("stepWorkingShelf", () => {
  const set = (...k: string[]) => new Set(k);

  it("stamps the turn start once and keeps it while running", () => {
    let { state } = stepWorkingShelf(EMPTY_WORKING_SHELF, set("a"), 1000, false);
    ({ state } = stepWorkingShelf(state, set("a"), 5000, false));
    expect(state.startedAt.get("a")).toBe(1000);
  });

  it("holds a stopped row for the grace, then drops it", () => {
    let { state } = stepWorkingShelf(EMPTY_WORKING_SHELF, set("a"), 0, false);
    let wakeAt: number | null;
    ({ state, wakeAt } = stepWorkingShelf(state, set(), 1000, false));
    expect(state.shown.has("a")).toBe(true);
    expect(wakeAt).toBe(1000 + WORKING_EXIT_GRACE_MS);
    ({ state } = stepWorkingShelf(state, set(), 1000 + WORKING_EXIT_GRACE_MS - 1, false));
    expect(state.shown.has("a")).toBe(true);
    ({ state, wakeAt } = stepWorkingShelf(state, set(), 1000 + WORKING_EXIT_GRACE_MS, false));
    expect(state.shown.has("a")).toBe(false);
    expect(wakeAt).toBeNull();
  });

  it("keeps it past the grace while hovered, drops it on leave", () => {
    let { state } = stepWorkingShelf(EMPTY_WORKING_SHELF, set("a"), 0, false);
    ({ state } = stepWorkingShelf(state, set(), 1000, true));
    ({ state } = stepWorkingShelf(state, set(), 60_000, true));
    expect(state.shown.has("a")).toBe(true);
    ({ state } = stepWorkingShelf(state, set(), 60_001, false));
    expect(state.shown.has("a")).toBe(false);
  });

  it("restamps the start when a lingering row runs again", () => {
    let { state } = stepWorkingShelf(EMPTY_WORKING_SHELF, set("a"), 0, false);
    ({ state } = stepWorkingShelf(state, set(), 1000, false));
    ({ state } = stepWorkingShelf(state, set("a"), 2000, false));
    expect(state.startedAt.get("a")).toBe(2000);
    expect(state.stoppedAt.has("a")).toBe(false);
  });
});

describe("routeRowOnly", () => {
  it("keeps only the open row", () => {
    const rows = [1, 2, 3].map((id) => ({ ws: repoA, tab: mkTab(id, "t"), ts: 0 }));
    expect(routeRowOnly(rows, (r) => r.tab.id === 2).map((r) => r.tab.id)).toEqual([2]);
    expect(routeRowOnly(rows, () => false)).toEqual([]);
  });
});
