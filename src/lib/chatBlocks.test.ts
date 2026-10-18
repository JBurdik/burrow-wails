import { describe, expect, it } from "vitest";
import { formatAnswers, parseQuestions, renderChatMd, renderVisual } from "./chatBlocks";

describe("chat rich blocks", () => {
  it("renders a composition with stats, a table and a bar chart, escaping values", () => {
    const html = renderVisual(JSON.stringify({
      title: "Tests",
      rows: [
        { stats: [{ label: "Failing", value: 3, tone: "bad" }] },
        { table: { rows: [{ name: "<a>", ms: 12 }] } },
        { chart: { mark: "bar", data: { values: [{ k: "x", v: 1 }, { k: "y", v: 2 }] }, encoding: { x: { field: "k" }, y: { field: "v" } } } },
      ],
    }))!;
    expect(html).toContain('class="cv-stat cv-bad"');
    expect(html).toContain("&lt;a&gt;");
    expect(html).not.toContain("<a>");
    expect(html.match(/<rect /g)).toHaveLength(2);
  });

  it("falls back to a plain code block for JSON that is invalid or still streaming", () => {
    expect(renderChatMd('```visual\n{"rows": [\n```')).toContain("<code");
    expect(renderChatMd('```visual\n{"rows": [')).toContain("<code");
  });

  it("renders a questions form, disabled once answered", () => {
    const md = '```questions\n{"questions":[{"question":"DB?","options":[{"label":"SQLite","recommended":true},"Postgres"]}]}\n```';
    expect(renderChatMd(md)).toContain('data-cq-action="send"');
    expect(renderChatMd(md)).toContain("Recommended");
    expect(renderChatMd(md, true)).toContain("Answered");
    expect(parseQuestions('{"questions":[{"question":"q","options":["a"]}]}')![0].options).toEqual([{ label: "a" }]);
  });

  it("formats answers, marking unanswered questions as skipped", () => {
    expect(formatAnswers([
      { question: "DB?", picked: ["SQLite"], other: "" },
      { question: "Name?", picked: [], other: " " },
    ], "ship it")).toBe("Answers:\n1. DB?\n   → SQLite\n2. Name?\n   → (skipped)\n\nNotes: ship it");
  });
});
