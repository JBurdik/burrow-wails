import { describe, it, expect } from "vitest";
import { nextSubAgentView } from "./subAgentView";

describe("nextSubAgentView", () => {
  it("stays null when nothing is open", () => {
    expect(nextSubAgentView(null, 1, 1, [10, 20])).toBeNull();
    expect(nextSubAgentView(null, 1, 2, [])).toBeNull();
  });

  it("survives an unrelated change (same thread, child still live)", () => {
    expect(nextSubAgentView(10, 1, 1, [10, 20])).toBe(10);
    // The list grew (a new sibling spawned) — the open child is unaffected.
    expect(nextSubAgentView(10, 1, 1, [10, 20, 30])).toBe(10);
  });

  it("clears when the active thread switches, even if a same-id child exists on the new thread", () => {
    expect(nextSubAgentView(10, 2, 1, [10, 20])).toBeNull();
  });

  it("clears when the open child is no longer among the thread's live children", () => {
    // Deleted directly, or cascade-deleted with its parent thread.
    expect(nextSubAgentView(10, 1, 1, [20, 30])).toBeNull();
    expect(nextSubAgentView(10, 1, 1, [])).toBeNull();
  });
});
