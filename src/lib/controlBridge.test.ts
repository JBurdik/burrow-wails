import { describe, it, expect } from "vitest";
import { subagentMessage } from "./chatTypes";

describe("subagentMessage", () => {
  it("is a system-info row that names the child chat", () => {
    // The transcript row is how a thread records that it delegated, and the id
    // is what makes it clickable — without it the row is just prose.
    const m = subagentMessage(42, "investigate the cache bug", "codex");
    expect(m.role).toBe("system-info");
    expect(m.subagentChatId).toBe(42);
    expect(m.subagentAgent).toBe("codex");
    expect(m.text).toContain("investigate the cache bug");
  });

  it("falls back to a generic label with no task text", () => {
    expect(subagentMessage(7, "", "claude").text).toContain("Sub-agent");
  });
});
