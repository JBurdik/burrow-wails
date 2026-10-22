import { describe, expect, it } from "vitest";
import { backTarget, inboxSections, isPhoneOnlyRoute, phoneScreen, showsBottomNav, type InboxRow } from "./phoneNav";

const row = (id: number, state: InboxRow["state"], ts = id): InboxRow => ({ wsId: 1, tabId: id, title: `t${id}`, state, ts });

describe("phoneNav", () => {
  it("shows the bottom nav on root screens only", () => {
    for (const r of ["welcome", "inbox", "project", "more"]) expect(showsBottomNav(r)).toBe(true);
    for (const r of ["tab", "workspace", "project-surface", "new", "dashboard"]) expect(showsBottomNav(r)).toBe(false);
  });

  it("maps routes to screens", () => {
    expect(phoneScreen("welcome")).toBe("threads");
    expect(phoneScreen("tab")).toBe("thread");
    expect(phoneScreen("workspace")).toBe("thread");
    expect(phoneScreen("new")).toBe("thread");
    expect(phoneScreen("project-surface")).toBe("project-surface");
    expect(phoneScreen("dashboard")).toBe("threads");
  });

  it("backs out of a detail to the root of its tab", () => {
    expect(backTarget("tab")).toBe("/");
    expect(backTarget("new")).toBe("/");
    expect(backTarget("project-surface")).toBe("/project");
  });

  it("knows which routes exist only on a phone", () => {
    expect(isPhoneOnlyRoute("inbox")).toBe(true);
    expect(isPhoneOnlyRoute("new")).toBe(true);
    expect(isPhoneOnlyRoute("tab")).toBe(false);
    expect(isPhoneOnlyRoute("welcome")).toBe(false);
  });

  it("groups the inbox, newest first, dropping idle and empty sections", () => {
    const s = inboxSections([row(1, "working"), row(2, "needs-input"), row(3, "idle"), row(4, "done-unread"), row(5, "error"), row(6, "needs-input")]);
    expect(s.map((x) => x.key)).toEqual(["needs", "done", "running"]);
    expect(s[0].rows.map((r) => r.tabId)).toEqual([6, 5, 2]);
    expect(s[1].rows.map((r) => r.tabId)).toEqual([4]);
    expect(s[2].rows.map((r) => r.tabId)).toEqual([1]);
    expect(inboxSections([row(1, "idle")])).toEqual([]);
  });
});
