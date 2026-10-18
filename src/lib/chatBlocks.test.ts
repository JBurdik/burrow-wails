import { describe, expect, it } from "vitest";
import { formatAnswers, pageSrcdoc, parseQuestions, renderChatMd, renderVisual } from "./chatBlocks";

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

describe("mermaid and page blocks", () => {
  it("emits a mermaid placeholder only for a closed fence, escaping the source", () => {
    const html = renderChatMd('```mermaid\nflowchart LR\n  A["<b>"] --> B\n```');
    expect(html).toContain('class="cm-mermaid"');
    expect(html).toContain(`data-mermaid-src="${encodeURIComponent('flowchart LR\n  A["<b>"] --> B')}"`);
    expect(html).toContain("A[&quot;&lt;b&gt;&quot;] --&gt; B</code>");
    expect(html).not.toContain("<b>");
    expect(renderChatMd("```mermaid\nflowchart LR\n  A --> B")).not.toContain("cm-mermaid");
    expect(renderChatMd("```mermaid\n```")).not.toContain("cm-mermaid");
  });

  it("renders a page card for an .html path only", () => {
    const card = renderVisual('{"rows":[{"page":{"title":"Mock <x>","path":"out/mock.html","description":"d"}}]}')!;
    expect(card).toContain('data-cv-page="out/mock.html"');
    expect(card).toContain("Mock &lt;x&gt;");
    expect(renderVisual('{"rows":[{"page":{"path":"/etc/passwd"}}]}')).not.toContain("data-cv-page");
  });

  it("puts the no-network CSP first, after any doctype", () => {
    expect(pageSrcdoc("<!DOCTYPE html><html><script>x</script>")).toMatch(/^<!DOCTYPE html><meta http-equiv="Content-Security-Policy" content="default-src 'none';/);
    expect(pageSrcdoc("<p>hi</p>")).toMatch(/^<meta http-equiv="Content-Security-Policy"[^>]*><p>hi<\/p>$/);
  });
});
