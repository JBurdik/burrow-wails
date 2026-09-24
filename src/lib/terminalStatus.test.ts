import { describe, expect, it } from "vitest";
import { summarizeChildActivity, threadAttentionSummary } from "./terminalStatus";

describe("summarizeChildActivity", () => {
  it("returns no badge for idle or already-read children", () => {
    expect(summarizeChildActivity(["idle", "done"])).toBeNull();
  });

  it("groups blocking children into a needs-input badge", () => {
    expect(summarizeChildActivity(["waiting", "permission"])).toEqual({
      state: "needs-input",
      count: 2,
    });
  });

  it("prefers an unread result over working while counting both children", () => {
    expect(summarizeChildActivity(["running", "review"])).toEqual({
      state: "done-unread",
      count: 2,
    });
  });

  it("keeps child errors highest priority", () => {
    expect(summarizeChildActivity(["running", "permission", "error"])).toEqual({
      state: "error",
      count: 3,
    });
  });
});

describe("threadAttentionSummary", () => {
  it("keeps the parent status while returning child attention to the live list", () => {
    expect(threadAttentionSummary("review", ["waiting"], true)).toEqual({
      status: "review",
      settled: false,
    });
  });

  it("restores the parent's settled decision after the child clears", () => {
    expect(threadAttentionSummary("review", ["done"], true)).toEqual({
      status: "review",
      settled: true,
    });
  });

  it("returns a settled parent to the live list for an unread child completion", () => {
    expect(threadAttentionSummary("idle", ["review"], true)).toEqual({
      status: "idle",
      settled: false,
    });
  });

  it("keeps the parent's own lifecycle status when only a child completed", () => {
    expect(threadAttentionSummary("running", ["review"], false)).toEqual({
      status: "running",
      settled: false,
    });
  });

  it("returns a settled parent to live while a child is working", () => {
    expect(threadAttentionSummary("idle", ["running"], true)).toEqual({
      status: "idle",
      settled: false,
    });
  });
});
