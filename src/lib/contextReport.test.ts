import { describe, it, expect } from "vitest";
import { parseContextReport } from "./contextReport";

// Trimmed from a real `/context` answer (Haiku 4.5, stream-json).
const REPORT = `## Context Usage

**Model:** claude-haiku-4-5-20251001
**Tokens:** 47k / 200k (23%)

### Estimated usage by category

| Category | Tokens | Percentage |
|----------|--------|------------|
| System prompt | 6.2k | 3.1% |
| System tools | 10k | 5.0% |
| Memory files | 462 | 0.2% |
| Messages | 28.3k | 14.1% |
| Free space | 153k | 76.5% |

### MCP Tools

| Tool | Server | Tokens |
|------|--------|--------|
| mcp__bproductive__add_comment | bproductive | 227 |
`;

describe("parseContextReport", () => {
  it("lifts the category table and stops before the MCP tool list", () => {
    const rows = parseContextReport(REPORT);
    expect(rows).toEqual([
      { label: "System prompt", tokens: "6.2k", pct: "3.1%" },
      { label: "System tools", tokens: "10k", pct: "5.0%" },
      { label: "Memory files", tokens: "462", pct: "0.2%" },
      { label: "Messages", tokens: "28.3k", pct: "14.1%" },
      { label: "Free space", tokens: "153k", pct: "76.5%" },
    ]);
  });

  it("is null for anything that is not a report, so a normal reply is not swallowed", () => {
    expect(parseContextReport("Sure, here is the context you asked about.")).toBeNull();
    expect(parseContextReport("")).toBeNull();
  });
});
