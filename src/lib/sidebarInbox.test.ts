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
