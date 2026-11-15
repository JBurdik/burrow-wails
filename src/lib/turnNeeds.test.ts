import { describe, expect, it } from "vitest";
import { parseTurnNeeds, turnPicks } from "./turnNeeds";

describe("turnNeeds", () => {
  it("parses only valid rows", () => {
    expect(parseTurnNeeds("")).toBeUndefined();
    expect(parseTurnNeeds("{")).toBeUndefined();
    expect(parseTurnNeeds('{"needs":"maybe"}')).toBeUndefined();
    expect(parseTurnNeeds('{"needs":"you","asks":[{"question":"Q","suggested":-1}]}')?.asks?.length).toBe(1);
  });

  it("turns the card's answers into picks", () => {
    const asks = [
      { question: "DB?", options: ["Postgres", "SQLite"], suggested: -1 },
      { question: "Skip me", suggested: -1 },
      { question: "Langs?", options: ["Go", "TS", "Swift"], multi: true, suggested: -1 },
      { question: "Other?", options: ["A"], suggested: -1 },
      { question: "Name?", suggested: -1 },
    ];
    expect(turnPicks(asks, ["Postgres", undefined, "Swift, Go", " mine ", " api "], [false, false, false, true, false])).toEqual([
      { index: 0, options: ["Postgres"] },
      { index: 2, options: ["Go", "Swift"] },
      { index: 3, text: "mine" },
      { index: 4, text: "api" },
    ]);
  });
});
