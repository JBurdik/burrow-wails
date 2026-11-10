import { describe, expect, it } from "vitest";
import { DETAILS_CARD_GAP, DETAILS_CARD_WIDTH, detailsLaneInset, parseNumstat, visibleRows, type DetailsContext } from "./threadDetails";

const base: DetailsContext = { kind: "chat", branch: "main", hasPr: false, changedFiles: 0, running: 0, scheduled: 0, lineage: 0 };

describe("visibleRows", () => {
  it("shows the always-on rows and hides the empty ones", () => {
    expect(visibleRows(base)).toEqual(["workspace", "scripts", "branch", "pr", "commit"]);
    expect(visibleRows({ ...base, kind: "tab" })).toEqual(["workspace", "scripts", "branch", "commit"]);
  });

  it("keeps a chat's linked PR even without a branch", () => {
    expect(visibleRows({ ...base, branch: "", hasLinkedPr: true })).toContain("pr");
  });

  it("drops every git row outside a repo", () => {
    expect(visibleRows({ ...base, branch: "", hasPr: true, changedFiles: 2 })).toEqual(["workspace", "scripts"]);
  });

  it("shows rows that have something to show", () => {
    const rows = visibleRows({ ...base, hasPr: true, changedFiles: 3, running: 1, scheduled: 2, lineage: 1 });
    expect(rows).toEqual(["workspace", "scripts", "branch", "pr", "commit", "changes", "running", "scheduled", "lineage"]);
  });

  it("keeps chat-only rows to chats", () => {
    for (const kind of ["tab", "workspace"] as const) {
      const rows = visibleRows({ ...base, kind, scheduled: 2, lineage: 1 });
      expect(rows).not.toContain("scheduled");
      expect(rows).not.toContain("lineage");
    }
  });
});

describe("parseNumstat", () => {
  it("one row per file, binary files count as nothing", () => {
    expect(parseNumstat("3\t1\ta.ts\n-\t-\timg.png\n")).toEqual([
      { path: "a.ts", added: 3, removed: 1 },
      { path: "img.png", added: 0, removed: 0 },
    ]);
    expect(parseNumstat("")).toEqual([]);
  });
});

describe("detailsLaneInset", () => {
  it("keeps a wide canvas's chat centred (no inset)", () => {
    // 1800 wide: a centred 760 lane ends at 1280, far left of the card (1472).
    expect(detailsLaneInset(1800)).toBe(0);
  });
  it("shifts the lane left, never under the card, when it would crowd it", () => {
    const w = 1200;
    const inset = detailsLaneInset(w)!;
    expect(inset).toBeGreaterThan(0);
    const laneWidth = Math.min(760, w - 48);
    const left = (w - inset - laneWidth) / 2;
    expect(left + laneWidth).toBeLessThanOrEqual(w - DETAILS_CARD_GAP - DETAILS_CARD_WIDTH - 32 + 0.001);
  });
  it("hides the card when the lane would get too narrow", () => {
    expect(detailsLaneInset(900)).toBeNull();
  });
  it("never jumps: the inset grows continuously as the canvas narrows", () => {
    let prev = detailsLaneInset(1600)!;
    for (let w = 1590; w >= 960; w -= 10) {
      const cur = detailsLaneInset(w)!;
      expect(Math.abs(cur - prev)).toBeLessThan(40);
      prev = cur;
    }
  });
});
