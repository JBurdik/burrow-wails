import { describe, it, expect } from "vitest";
import { shouldRestamp, stampKey } from "./terminalTabs";

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

describe("stampKey", () => {
  it("keys a chat by its stable chatId, not its re-minted pty id", () => {
    // Terminal re-mints a chat tab's numeric id on every restore, so the id is
    // not an identity across a restart — that is what made every thread read
    // "now" after opening the app, and pruned the real stamps as orphans.
    expect(stampKey({ id: 7, chatId: 161 })).toBe(stampKey({ id: 42, chatId: 161 }));
  });

  it("keys a plain terminal by its pty id", () => {
    expect(stampKey({ id: 7 })).toBe("7");
    expect(stampKey({ id: 7 })).not.toBe(stampKey({ id: 8 }));
  });
});
