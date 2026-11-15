import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@/lib/chatTypes";
import cases from "../../fixtures/transcript-rows.json";
import { toolGroupLabel, transcriptRows, type TranscriptRow } from "./transcriptRows";

// Shared with ios/Tests/TranscriptRowsTests.swift: both implementations must
// give these outputs.
function normalize(r: TranscriptRow) {
  switch (r.kind) {
    case "message": return { message: r.message.id };
    case "tools": return { tools: r.items.map((m) => m.id), label: toolGroupLabel(r.items), failed: r.items.some((m) => !!m.toolFailed) };
    case "worked": return { worked: r.turn, tools: r.tools, folded: r.folded };
  }
}

describe("transcriptRows (fixtures/transcript-rows.json)", () => {
  for (const c of cases as { name: string; settledLast: boolean; foldOverride?: Record<string, boolean>; rows: unknown[]; expect: unknown[] }[]) {
    it(c.name, () => {
      const rows = transcriptRows(c.rows as ChatMessage[], { settledLast: c.settledLast, foldOverride: c.foldOverride });
      expect(rows.map(normalize)).toEqual(c.expect);
    });
  }
});
