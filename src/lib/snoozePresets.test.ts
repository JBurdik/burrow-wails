import { describe, it, expect } from "vitest";
import { formatWakeTime, snoozePresets } from "./snoozePresets";

const local = (y: number, mo: number, d: number, h: number, mi = 0) => new Date(y, mo - 1, d, h, mi).getTime();
const labels = (now: number) => snoozePresets(now).map((p) => p.label);
const until = (now: number, label: string) => snoozePresets(now).find((p) => p.label === label)?.until;

describe("snoozePresets", () => {
  // 2026-10-28 is a Wednesday, 2026-11-02 a Monday.
  it("offers every preset in the morning", () => {
    const now = local(2026, 10, 28, 10);
    expect(labels(now)).toEqual(["1 hour", "This evening", "Tomorrow 9:00", "Next Monday 9:00"]);
    expect(until(now, "1 hour")).toBe(now + 3_600_000);
    expect(until(now, "This evening")).toBe(local(2026, 10, 28, 18));
    expect(until(now, "Tomorrow 9:00")).toBe(local(2026, 10, 29, 9));
    expect(until(now, "Next Monday 9:00")).toBe(local(2026, 11, 2, 9));
  });

  it("hides this evening from 18:00 on", () => {
    expect(labels(local(2026, 10, 28, 18))).not.toContain("This evening");
    expect(labels(local(2026, 10, 28, 21, 30))).not.toContain("This evening");
    expect(labels(local(2026, 10, 28, 17, 59))).toContain("This evening");
  });

  it("from a Monday, next Monday is a week later", () => {
    expect(until(local(2026, 11, 2, 8), "Next Monday 9:00")).toBe(local(2026, 11, 9, 9));
  });

  it("from a Sunday, next Monday is tomorrow", () => {
    expect(until(local(2026, 11, 1, 12), "Next Monday 9:00")).toBe(local(2026, 11, 2, 9));
  });

  it("tomorrow crosses a month end", () => {
    expect(until(local(2026, 10, 31, 12), "Tomorrow 9:00")).toBe(local(2026, 11, 1, 9));
  });
});

describe("formatWakeTime", () => {
  it("shows only the time today", () => {
    expect(formatWakeTime(local(2026, 10, 28, 18), local(2026, 10, 28, 10))).toBe("18:00");
  });
  it("shows the weekday within a week", () => {
    expect(formatWakeTime(local(2026, 11, 2, 9), local(2026, 10, 28, 10))).toBe("Mon 9:00");
  });
});
