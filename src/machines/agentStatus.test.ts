/**
 * Unit tests for agentStatusMachine.
 * Uses XState's createActor directly — no Vue, no DOM.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createActor } from "xstate";
import { agentStatusMachine } from "./agentStatus";

/** `isAgent: true` mimics a Claude/Codex leaf: hooks own status, poll is ignored. */
function actor(isAgent = false) {
  const a = createActor(agentStatusMachine, { input: { isAgent } });
  a.start();
  return a;
}

describe("agentStatusMachine", () => {
  describe("basic transitions", () => {
    it("starts in idle", () => {
      const a = actor();
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("idle → START → running", () => {
      const a = actor();
      a.send({ type: "START" });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("running → WAIT → waiting", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "WAIT" });
      expect(a.getSnapshot().value).toBe("waiting");
    });

    it("running → PERMISSION_REQUEST → permission", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "PERMISSION_REQUEST" });
      expect(a.getSnapshot().value).toBe("permission");
    });

    it("idle → PERMISSION_REQUEST → permission for a native approval", () => {
      const a = actor(true);
      a.send({ type: "PERMISSION_REQUEST" });
      expect(a.getSnapshot().value).toBe("permission");
    });

    it("waiting → RESUME → running", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "WAIT" });
      a.send({ type: "RESUME" });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("permission → RESUME → running", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "PERMISSION_REQUEST" });
      a.send({ type: "RESUME" });
      expect(a.getSnapshot().value).toBe("running");
    });
  });

  describe("STOP guard: isWatching", () => {
    it("STOP watching=true → done", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: true });
      expect(a.getSnapshot().value).toBe("done");
    });

    it("STOP watching=false → review", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: false });
      expect(a.getSnapshot().value).toBe("review");
    });

    it("STOP from waiting watching=true → done", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "WAIT" });
      a.send({ type: "STOP", watching: true });
      expect(a.getSnapshot().value).toBe("done");
    });

    it("STOP from permission watching=false → review", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "PERMISSION_REQUEST" });
      a.send({ type: "STOP", watching: false });
      expect(a.getSnapshot().value).toBe("review");
    });
  });

  describe("done: 4s auto-clear", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("done → idle after 4 s", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: true });
      expect(a.getSnapshot().value).toBe("done");
      vi.advanceTimersByTime(4000);
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("done → MARK_SEEN → idle (before timer)", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: true });
      a.send({ type: "MARK_SEEN" });
      expect(a.getSnapshot().value).toBe("idle");
    });
  });

  describe("review: persists until MARK_SEEN", () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    it("review stays after 10 s", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: false });
      vi.advanceTimersByTime(10_000);
      expect(a.getSnapshot().value).toBe("review");
    });

    it("review → MARK_SEEN → idle", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: false });
      a.send({ type: "MARK_SEEN" });
      expect(a.getSnapshot().value).toBe("idle");
    });
  });

  describe("error", () => {
    it("running → FAIL → error with detail", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "FAIL", detail: "rate_limit" });
      expect(a.getSnapshot().value).toBe("error");
      expect(a.getSnapshot().context.detail).toBe("rate_limit");
    });

    it("error → START → running, detail cleared", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "FAIL", detail: "overloaded" });
      a.send({ type: "START" });
      expect(a.getSnapshot().value).toBe("running");
      expect(a.getSnapshot().context.detail).toBeUndefined();
    });

    it("error → MARK_SEEN → idle", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "FAIL" });
      a.send({ type: "MARK_SEEN" });
      expect(a.getSnapshot().value).toBe("idle");
    });
  });

  describe("INTERRUPT", () => {
    it("running → INTERRUPT → idle", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "INTERRUPT" });
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("waiting → INTERRUPT → idle", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "WAIT" });
      a.send({ type: "INTERRUPT" });
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("permission → INTERRUPT → idle", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "PERMISSION_REQUEST" });
      a.send({ type: "INTERRUPT" });
      expect(a.getSnapshot().value).toBe("idle");
    });
  });

  describe("new turn from terminal states", () => {
    it("review → START → running", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "STOP", watching: false });
      a.send({ type: "START" });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("error → START → running, detail cleared", () => {
      const a = actor();
      a.send({ type: "START" });
      a.send({ type: "FAIL", detail: "billing_error" });
      a.send({ type: "START" });
      expect(a.getSnapshot().value).toBe("running");
      expect(a.getSnapshot().context.detail).toBeUndefined();
    });
  });
});

// ── Poll channel (foreground process, 2 s) ────────────────────────────────────
// These are the cases that used to live in terminalStatus.applyBusy/applyNeedsInput.

describe("poll channel", () => {
  describe("plain command (non-agent)", () => {
    it("BUSY → running, NOT_BUSY while watching → done", () => {
      const a = actor(false);
      a.send({ type: "BUSY" });
      expect(a.getSnapshot().value).toBe("running");
      a.send({ type: "NOT_BUSY", watching: true });
      expect(a.getSnapshot().value).toBe("done");
    });

    it("NOT_BUSY while away → review (persists)", () => {
      const a = actor(false);
      a.send({ type: "BUSY" });
      a.send({ type: "NOT_BUSY", watching: false });
      expect(a.getSnapshot().value).toBe("review");
    });

    it("NEEDS_INPUT → waiting, then resumes on needs:false", () => {
      const a = actor(false);
      a.send({ type: "BUSY" });
      a.send({ type: "NEEDS_INPUT", needs: true });
      expect(a.getSnapshot().value).toBe("waiting");
      a.send({ type: "NEEDS_INPUT", needs: false });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("NEEDS_INPUT at an idle prompt is a no-op (not busy → no dot)", () => {
      const a = actor(false);
      a.send({ type: "NEEDS_INPUT", needs: true });
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("a waiting command that exits still settles", () => {
      const a = actor(false);
      a.send({ type: "BUSY" });
      a.send({ type: "NEEDS_INPUT", needs: true });
      a.send({ type: "NOT_BUSY", watching: false });
      expect(a.getSnapshot().value).toBe("review");
    });
  });

  describe("agent leaf — hooks are the sole authority", () => {
    it("BUSY never fabricates running (the stuck-orange-dot bug)", () => {
      const a = actor(true);
      a.send({ type: "BUSY" });
      expect(a.getSnapshot().value).toBe("idle");
    });

    it("NOT_BUSY cannot settle a live agent turn", () => {
      const a = actor(true);
      a.send({ type: "START" });
      a.send({ type: "NOT_BUSY", watching: true });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("NEEDS_INPUT cannot drag a running agent into waiting", () => {
      const a = actor(true);
      a.send({ type: "START" });
      a.send({ type: "NEEDS_INPUT", needs: true });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("SET_AGENT flips the guard mid-flight", () => {
      const a = actor(false);
      a.send({ type: "SET_AGENT", isAgent: true });
      a.send({ type: "BUSY" });
      expect(a.getSnapshot().value).toBe("idle");
      a.send({ type: "SET_AGENT", isAgent: false });
      a.send({ type: "BUSY" });
      expect(a.getSnapshot().value).toBe("running");
    });

    it("INTERRUPT (Ctrl+C / dead-PTY watchdog) settles straight to idle", () => {
      const a = actor(true);
      a.send({ type: "START" });
      a.send({ type: "INTERRUPT" });
      expect(a.getSnapshot().value).toBe("idle");
    });
  });
});

// ── Side-effect actions ───────────────────────────────────────────────────────

describe("injected actions", () => {
  it("fires playWaiting once per entry into waiting, not on a repeated WAIT", () => {
    const playWaiting = vi.fn();
    const a = createActor(
      agentStatusMachine.provide({ actions: { playWaiting } }),
      { input: { isAgent: true } },
    );
    a.start();
    a.send({ type: "START" });
    a.send({ type: "WAIT" });
    a.send({ type: "WAIT" });
    expect(playWaiting).toHaveBeenCalledTimes(1);
  });

  it("done fires onDone; review fires onReview", () => {
    const onDone = vi.fn();
    const onReview = vi.fn();
    const mk = () => {
      const a = createActor(
        agentStatusMachine.provide({ actions: { onDone, onReview } }),
        { input: { isAgent: true } },
      );
      a.start();
      a.send({ type: "START" });
      return a;
    };
    mk().send({ type: "STOP", watching: true });
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onReview).not.toHaveBeenCalled();
    mk().send({ type: "STOP", watching: false });
    expect(onReview).toHaveBeenCalledTimes(1);
  });
});
