import { describe, it, expect } from "vitest";

/** The rule pruneActiveByWs applies, extracted so it can be checked without a
 *  store: a saved selection survives only if it names a live THREAD of that
 *  same workspace. */
function keepsSelection(
  sessions: Array<{ id: number; workspaceId: number; parentChatId?: number }>,
  wsId: number,
  chatId: number,
): boolean {
  return sessions.some((s) => s.id === chatId && s.workspaceId === wsId && !s.parentChatId);
}

const sessions = [
  { id: 10, workspaceId: 2 },                    // a thread
  { id: 11, workspaceId: 2, parentChatId: 10 },  // its sub-agent
  { id: 20, workspaceId: 5 },                    // a thread elsewhere
];

describe("pruneActiveByWs's rule", () => {
  it("keeps a thread of that workspace", () => {
    expect(keepsSelection(sessions, 2, 10)).toBe(true);
  });

  it("drops a sub-agent — the slot means THREAD, and activeSession filters it out anyway", () => {
    // This is the stale value that made clicking a thread land on the first one.
    expect(keepsSelection(sessions, 2, 11)).toBe(false);
  });

  it("drops a chat that belongs to a different workspace", () => {
    expect(keepsSelection(sessions, 2, 20)).toBe(false);
  });

  it("drops a chat that no longer exists", () => {
    expect(keepsSelection(sessions, 2, 999)).toBe(false);
  });
});
