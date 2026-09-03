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

  it("stamps when a turn starts", () => {
    const running = { ...tab, status: "running" as const };
    expect(shouldRestamp(tab, running, true)).toBe(true);
  });

  it("does not stamp mid-turn or on the way out of a turn", () => {
    const running = { ...tab, status: "running" as const };
    // status churn inside one turn, and the turn ending, both leave order alone
    expect(shouldRestamp(running, { ...tab, status: "waiting" }, true)).toBe(false);
    expect(shouldRestamp(running, { ...tab, status: "running", round: 2 }, true)).toBe(false);
    expect(shouldRestamp(running, { ...tab, status: "review" }, true)).toBe(false);
    expect(shouldRestamp({ ...tab, title: "old" }, tab, true)).toBe(false);
  });

  it("ignores a sync that changed nothing", () => {
    expect(shouldRestamp({ ...tab }, tab, true)).toBe(false);
  });
});
