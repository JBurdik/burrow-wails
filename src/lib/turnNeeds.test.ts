import { describe, expect, it } from "vitest";
import { composeAnswers, composeNext, parseTurnNeeds } from "./turnNeeds";

describe("turnNeeds", () => {
  it("parses only valid rows", () => {
    expect(parseTurnNeeds("")).toBeUndefined();
    expect(parseTurnNeeds("{")).toBeUndefined();
    expect(parseTurnNeeds('{"needs":"maybe"}')).toBeUndefined();
    expect(parseTurnNeeds('{"needs":"you","asks":[{"question":"Q","suggested":-1}]}')?.asks?.length).toBe(1);
  });

  it("sends a lone answer bare", () => {
    expect(composeAnswers([{ question: "Ship?", suggested: -1 }], ["Yes"], "also bump")).toBe("Yes\n\nalso bump");
  });

  it("quotes each answered question and skips unanswered ones", () => {
    const asks = [
      { question: "DB?", suggested: -1 },
      { question: "Skip me", suggested: -1 },
      { question: "Name?", suggested: -1 },
    ];
    expect(composeAnswers(asks, ["Postgres", undefined, " api "])).toBe("> DB?\nPostgres\n\n> Name?\napi");
  });

  it("lists several follow-ups", () => {
    expect(composeNext(["Open PR"])).toBe("Open PR");
    expect(composeNext(["A", "B"], "thanks")).toBe("- A\n- B\n\nthanks");
  });

  it("sends a run result kept on the card as that ask's answer", () => {
    const asks = [
      { question: "Ship?", options: ["Yes", "No"], suggested: -1 },
      { question: "Run the migration", suggested: -1, run: { command: "make migrate", state: "done" as const, exit: 0, reply: "migrated 3" } },
    ];
    expect(composeAnswers(asks, ["Yes", undefined])).toBe("> Ship?\nYes\n\n> Run the migration\nmigrated 3");
  });
});
