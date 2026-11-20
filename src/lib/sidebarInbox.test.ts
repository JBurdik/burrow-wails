import { describe, expect, it } from "vitest";
import { needsYouFirst, nextNeedsYou } from "./sidebarGroups";

const rows = ["a", "B", "c", "D", "e"]; // capitals need you
const needs = (r: string) => r === r.toUpperCase();

describe("sidebar inbox", () => {
  it("puts rows that need you first, keeping the rest in order", () => {
    expect(needsYouFirst(rows, needs)).toEqual({ rows: ["B", "D", "a", "c", "e"], needs: 2 });
    expect(needsYouFirst(["a", "c"], needs)).toEqual({ rows: ["a", "c"], needs: 0 });
  });

  it("steps to the next waiting row and wraps", () => {
    expect(nextNeedsYou(rows, needs, () => false)).toBe("B");
    expect(nextNeedsYou(rows, needs, (r) => r === "B")).toBe("D");
    expect(nextNeedsYou(rows, needs, (r) => r === "D")).toBe("B");
    expect(nextNeedsYou(rows, needs, (r) => r === "c")).toBe("D");
  });

  it("is caught up when nothing else waits", () => {
    expect(nextNeedsYou(["a", "c"], needs, () => false)).toBeNull();
    expect(nextNeedsYou(["a", "B"], needs, (r) => r === "B")).toBeNull();
  });
});

import { liftInbox } from "./sidebarGroups";

describe("global inbox", () => {
  const g = (repoId: number, rows: string[]) => ({ repoId, rows });
  // N* = needs you, U* = unread, lower case = neither
  const needs = (r: string) => r.startsWith("N");
  const unread = (r: string) => r.startsWith("U");

  it("lifts needs-you first, then unread, out of project groups", () => {
    const out = liftInbox([g(-1, ["Npin"]), g(1, ["a", "U1", "N1"]), g(2, ["N2"]), g(3, [])], needs, unread, (id) => id < 0);
    expect(out.inbox).toEqual(["N1", "N2", "U1"]);
    expect(out.needs).toBe(2);
    // Pinned keeps its row; repo 2 emptied by the lift goes; repo 3 was empty and stays.
    expect(out.groups).toEqual([g(-1, ["Npin"]), g(1, ["a"]), g(3, [])]);
  });
});
