import { describe, it, expect } from "vitest";
import { canSideChat, childrenOf, sideChatsOf, subagentArchivable, topLevel } from "./chatTree";
import type { ClaudeSession } from "./claudeChats";

const s = (p: Partial<ClaudeSession> & { id: number }): ClaudeSession => ({ workspaceId: 1, busy: false, ...p }) as ClaudeSession;

const sessions = [
  s({ id: 1, transport: "claude-cli" }),
  s({ id: 2, parentChatId: 1 }), // sub-agent
  s({ id: 3, parentChatId: 1, kind: "side" }),
  s({ id: 4, parentChatId: 1, kind: "side" }),
  s({ id: 5, parentChatId: 1, kind: "side", archivedAt: 9 }),
];

describe("side chats", () => {
  it("are not sub-agents and not threads", () => {
    expect(childrenOf(sessions, 1).map((c) => c.id)).toEqual([2]);
    expect(topLevel(sessions, 1).map((c) => c.id)).toEqual([1]);
  });

  it("are listed newest first, archived ones dropped", () => {
    expect(sideChatsOf(sessions, 1).map((c) => c.id)).toEqual([4, 3]);
  });

  it("exist only for Claude and Codex chats", () => {
    expect(canSideChat(s({ id: 1, transport: "claude-cli" }))).toBe(true);
    expect(canSideChat(s({ id: 1, transport: "codex-app-server" }))).toBe(true);
    expect(canSideChat(s({ id: 1 }))).toBe(true); // legacy row = Claude
    expect(canSideChat(s({ id: 1, transport: "acp", agentKind: "gemini" }))).toBe(false);
    expect(canSideChat(s({ id: 1, agentKind: "codex" }))).toBe(false); // Go drives it as generic ACP
    expect(canSideChat(s({ id: 3, parentChatId: 1, kind: "side" }))).toBe(false);
    expect(canSideChat(undefined)).toBe(false);
  });

  it("auto-archive without a collected result", () => {
    const phase = { state: "done", turn_ended_at: 100, updated_at: 100 } as never;
    expect(subagentArchivable(s({ id: 3, parentChatId: 1, kind: "side" }), phase, 200, false, 200 + 31 * 60_000)).toBe(true);
    expect(subagentArchivable(s({ id: 2, parentChatId: 1 }), phase, 200, false, 200 + 31 * 60_000)).toBe(false);
  });
});
