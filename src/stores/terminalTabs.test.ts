import { describe, it, expect } from "vitest";
import { shouldRestamp } from "./terminalTabs";

const tab = { status: "idle" as const, title: "Terminal 1", round: 0 };

describe("shouldRestamp", () => {
  it("stamps a tab that has none yet", () => {
    expect(shouldRestamp(undefined, tab, false)).toBe(true);
  });

  it("leaves a restored tab alone on the first sync after a restart", () => {
    // `before` is undefined for every tab right after a reload; restamping here
    // would flatten the persisted order into one timestamp.
    expect(shouldRestamp(undefined, tab, true)).toBe(false);
  });

  it("stamps on a real change", () => {
    expect(shouldRestamp({ ...tab, status: "running" }, tab, true)).toBe(true);
    expect(shouldRestamp({ ...tab, title: "old" }, tab, true)).toBe(true);
    expect(shouldRestamp({ ...tab, round: 1 }, tab, true)).toBe(true);
  });

  it("ignores a sync that changed nothing", () => {
    expect(shouldRestamp({ ...tab }, tab, true)).toBe(false);
  });
});
