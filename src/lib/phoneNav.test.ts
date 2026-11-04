import { describe, expect, it } from "vitest";
import { backTarget, isPhoneOnlyRoute, phoneScreen, showsBottomNav } from "./phoneNav";

describe("phoneNav", () => {
  it("shows the bottom nav on root screens only", () => {
    for (const r of ["welcome", "project", "more"]) expect(showsBottomNav(r)).toBe(true);
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
    expect(isPhoneOnlyRoute("project")).toBe(true);
    expect(isPhoneOnlyRoute("new")).toBe(true);
    expect(isPhoneOnlyRoute("tab")).toBe(false);
    expect(isPhoneOnlyRoute("welcome")).toBe(false);
  });
});
