import { describe, expect, it } from "vitest";
import { formatReset, peakPercent, usageLevel, type PlanUsage } from "./planUsage";

describe("planUsage", () => {
  it("peaks over ok providers only", () => {
    const u: PlanUsage = {
      providers: [
        { kind: "claude", plan: "team", status: "ok", rows: [{ label: "a", percent: 4 }, { label: "b", percent: 16 }] },
        { kind: "codex", plan: "", status: "unavailable", rows: [{ label: "c", percent: 99 }] },
      ],
    };
    expect(peakPercent(u)).toBe(16);
    expect(peakPercent({ providers: [] })).toBeNull();
    expect(peakPercent(null)).toBeNull();
  });

  it("levels", () => {
    expect(usageLevel(10)).toBe("ok");
    expect(usageLevel(75)).toBe("warning");
    expect(usageLevel(95)).toBe("exceeded");
  });

  it("formats resets", () => {
    const now = Date.parse("2026-10-27T10:00:00Z");
    expect(formatReset("2026-10-27T10:20:00Z", now)).toBe("in 20m");
    expect(formatReset("2026-10-27T12:30:00Z", now)).toBe("in 2h 30m");
    expect(formatReset("2026-10-27T09:00:00Z", now)).toBe("now");
    expect(formatReset(undefined, now)).toBe("");
    expect(formatReset("2026-10-30T10:00:00Z", now)).not.toBe("");
  });
});
