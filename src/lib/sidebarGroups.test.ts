import { describe, it, expect } from "vitest";
import { buildActivityRows } from "./sidebarGroups";
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
