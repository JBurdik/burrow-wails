/**
 * The phone's status derivation is the desktop's, not a second copy of it.
 *
 * It used to be a copy: store.ts subscribed to the legacy `pty-hook-*`
 * channel, kept its own `statuses` map and its own 4-second `done` timer.
 * That is two state machines for one question, and they drifted — the phone's
 * `review` could not be cleared by the desktop looking at the tab, and a
 * `done` that arrived while the phone was asleep was simply never seen.
 *
 * Since phase 6 both read the server's phase through `displayStatus`, and
 * what differs is only the read receipt — which is the point: whether a
 * finished turn still needs looking at is per-device.
 */
import { describe, it, expect } from "vitest";
import { displayStatus, type Phase } from "@/runtime/displayStatus";

const phase = (over: Partial<Phase>): Phase => ({
  state: "idle",
  is_agent: true,
  turn_ended_at: 0,
  updated_at: 0,
  ...over,
});

describe("the phone's terminal status", () => {
  it("shows review for a turn that ended after this device last looked", () => {
    // The desktop may have been staring at this tab the whole time. That is
    // the desktop's receipt, not the phone's.
    const p = phase({ state: "done", turn_ended_at: 2_000 });
    expect(displayStatus(p, 1_000, false)).toBe("review");
  });

  it("shows nothing to review once this device has looked", () => {
    const p = phase({ state: "done", turn_ended_at: 2_000 });
    expect(displayStatus(p, 3_000, false)).toBe("idle");
  });

  it("shows the transient done while the phone is actually on the tab", () => {
    const p = phase({ state: "done", turn_ended_at: 2_000 });
    expect(displayStatus(p, 1_000, true)).toBe("done");
  });

  it("surfaces a blocked agent as needing the user", () => {
    expect(displayStatus(phase({ state: "waiting_approval" }), 0, false)).toBe("permission");
    expect(displayStatus(phase({ state: "waiting_input" }), 0, false)).toBe("waiting");
  });

  it("keeps a failure red even while the phone is on the tab", () => {
    // A failure does not get the transient-lime treatment a success does:
    // being on the screen is not the same as having taken it in. Only the
    // read receipt clears it — which is why it is a receipt and not a state.
    const failed = phase({ state: "failed", turn_ended_at: 2_000 });
    expect(displayStatus(failed, 1_000, true)).toBe("error");
    expect(displayStatus(failed, 1_000, false)).toBe("error");
    expect(displayStatus(failed, 3_000, false)).toBe("idle");
  });

  it("treats a leaf it has never heard of as idle, not as missing", () => {
    // A phone paints from the snapshot before any live event arrives, and a
    // tab with no phase yet must render, not blank.
    expect(displayStatus(undefined, 0, false)).toBe("idle");
  });
});
