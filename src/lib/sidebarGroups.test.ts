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
  const activityAt = (_ws: number, tabId: number) => stamps[tabId] ?? 0;
  const open = [repoA, wtA1, repoB];

  it("flattens every open workspace's tabs, newest first", () => {
    const { live, snoozed } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, archivedIds: [], filterProjectId: null });
    expect(live.map((r) => r.tab.id)).toEqual([101, 200, 100, 300]);
    expect(snoozed).toEqual([]);
  });

  it("snoozes tabs of an archived workspace", () => {
    const { live, snoozed } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, archivedIds: [10], filterProjectId: null });
    expect(live.map((r) => r.tab.id)).toEqual([101, 100, 300]);
    expect(snoozed.map((r) => r.tab.id)).toEqual([200]);
  });

  it("snoozes a worktree's tabs when its parent repo is archived", () => {
    const { live, snoozed } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, archivedIds: [1], filterProjectId: null });
    expect(live.map((r) => r.tab.id)).toEqual([300]);
    expect(snoozed.map((r) => r.tab.id)).toEqual([101, 200, 100]);
  });

  it("filters by repo, keeping that repo's worktree rows", () => {
    const { live } = buildActivityRows({ openedWorkspaces: open, tabsByWs, activityAt, archivedIds: [], filterProjectId: 1 });
    expect(live.map((r) => r.tab.id)).toEqual([101, 200, 100]);
  });

  it("snoozes a snoozed tab, leaving its siblings live", () => {
    const { live, snoozed } = buildActivityRows({
      openedWorkspaces: open, tabsByWs, activityAt,
      archivedIds: [], snoozedKeys: ["1:101", "10:200"], filterProjectId: null,
    });
    expect(live.map((r) => r.tab.id)).toEqual([100, 300]);
    expect(snoozed.map((r) => r.tab.id)).toEqual([101, 200]);
  });

  it("keeps never-stamped tabs in a stable order", () => {
    const { live } = buildActivityRows({
      openedWorkspaces: [repoB],
      tabsByWs: { 2: [mkTab(300, "b1"), mkTab(301, "b2")] },
      activityAt: () => 0, archivedIds: [], filterProjectId: null,
    });
    expect(live.map((r) => r.tab.id)).toEqual([301, 300]);
  });

  it("buckets a settled chat tab separately from live and snoozed", () => {
    const { live, settledChats, snoozed } = buildActivityRows({
      openedWorkspaces: [repoA],
      tabsByWs: { 1: [mkTab(100, "a1"), mkTab(101, "a2", { isChat: true, settled: true })] },
      activityAt, archivedIds: [], filterProjectId: null,
    });
    expect(live.map((r) => r.tab.id)).toEqual([100]);
    expect(settledChats.map((r) => r.tab.id)).toEqual([101]);
    expect(snoozed).toEqual([]);
  });

  it("a snoozed workspace's tabs win over per-chat settled status", () => {
    const { settledChats, snoozed } = buildActivityRows({
      openedWorkspaces: [wtA1],
      tabsByWs: { 10: [mkTab(200, "wt", { isChat: true, settled: true })] },
      activityAt, archivedIds: [10], filterProjectId: null,
    });
    expect(settledChats).toEqual([]);
    expect(snoozed.map((r) => r.tab.id)).toEqual([200]);
  });
});
