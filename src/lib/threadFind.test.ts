import { describe, expect, it } from "vitest";
import { findMatches, findableText, formatFindCount, occurrences, stepFindIndex } from "./threadFind";
import type { ChatMessage } from "./chatTypes";

const msgs: ChatMessage[] = [
  { id: 1, role: "user", text: "Fix the Parser" },
  { id: 2, role: "assistant", text: "parser fixed; parser tests pass" },
  { id: 3, role: "tool", text: "Bash", toolInput: { command: "go test ./parser" }, toolOutput: "ok" },
  { id: 4, role: "tool", text: "ExitPlanMode", toolInput: { plan: "1. Rewrite PARSER" } },
  { id: 5, role: "queued", text: "parser again" },
  { id: 6, role: "thinking", text: "nothing here" },
];

describe("threadFind", () => {
  it("matches case-insensitively across text, tool input/output and plans, skipping queued", () => {
    expect(findMatches(msgs, "parser")).toEqual([
      { id: 1, occurrence: 0 },
      { id: 2, occurrence: 0 },
      { id: 2, occurrence: 1 },
      { id: 3, occurrence: 0 },
      { id: 4, occurrence: 0 },
    ]);
    expect(findMatches(msgs, "ok")).toEqual([{ id: 3, occurrence: 0 }]);
    expect(findMatches(msgs, "")).toEqual([]);
  });

  it("counts non-overlapping occurrences", () => {
    expect(occurrences("aaaa", "aa")).toEqual([0, 2]);
    expect(occurrences("AbAB", "ab")).toEqual([0, 2]);
  });

  it("searches no tool input when there is none", () => {
    expect(findableText({ id: 9, role: "tool", text: "Read", toolInput: {} })).toBe("Read\n\n");
  });

  it("steps with wraparound and formats the counter", () => {
    expect(stepFindIndex(2, 3, 1)).toBe(0);
    expect(stepFindIndex(0, 3, -1)).toBe(2);
    expect(stepFindIndex(9, 3, 0)).toBe(2);
    expect(stepFindIndex(0, 0, 1)).toBe(0);
    expect(formatFindCount(0, 0)).toBe("0/0");
    expect(formatFindCount(1, 5)).toBe("2/5");
  });
});
