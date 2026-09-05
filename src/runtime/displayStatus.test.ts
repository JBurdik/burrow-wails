import { describe, it, expect } from "vitest";
import { displayStatus, shouldMarkSeen, type Phase } from "./displayStatus";

const phase = (p: Partial<Phase>): Phase => ({
  state: "idle",
  is_agent: false,
  turn_ended_at: 0,
  updated_at: 0,
  ...p,
});

describe("displayStatus", () => {
  it("maps in-flight phases straight through", () => {
    expect(displayStatus(phase({ state: "running" }), 0, true)).toBe("running");
    expect(displayStatus(phase({ state: "waiting_input" }), 0, true)).toBe("waiting");
    expect(displayStatus(phase({ state: "waiting_approval" }), 0, true)).toBe("permission");
  });

  it("shows done while watching and review while away", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(displayStatus(p, 0, true)).toBe("done");
    expect(displayStatus(p, 0, false)).toBe("review");
  });

  it("clears once the turn has been seen", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(displayStatus(p, 100, false)).toBe("idle");
    expect(displayStatus(p, 200, false)).toBe("idle");
  });

  it("keeps a failed turn until it is seen, watching or not", () => {
    const p = phase({ state: "failed", detail: "billing_error", turn_ended_at: 100 });
    expect(displayStatus(p, 0, true)).toBe("error");
    expect(displayStatus(p, 0, false)).toBe("error");
    expect(displayStatus(p, 100, true)).toBe("idle");
  });

  it("treats a newer turn as unseen again", () => {
    const p = phase({ state: "done", turn_ended_at: 300 });
    expect(displayStatus(p, 100, false)).toBe("review");
  });

  it("settles a stale pty without shouting about it", () => {
    expect(displayStatus(phase({ state: "stale", turn_ended_at: 100 }), 0, false)).toBe("idle");
  });

  it("is idle for an unknown id", () => {
    expect(displayStatus(undefined, 0, false)).toBe("idle");
  });
});

describe("shouldMarkSeen", () => {
  it("marks a finished turn seen only while watching", () => {
    const p = phase({ state: "done", turn_ended_at: 100 });
    expect(shouldMarkSeen(p, true)).toBe(true);
    expect(shouldMarkSeen(p, false)).toBe(false);
  });

  it("never auto-marks a failed turn", () => {
    const p = phase({ state: "failed", turn_ended_at: 100 });
    expect(shouldMarkSeen(p, true)).toBe(false);
  });

  it("never marks an in-flight turn", () => {
    expect(shouldMarkSeen(phase({ state: "running" }), true)).toBe(false);
  });
});
