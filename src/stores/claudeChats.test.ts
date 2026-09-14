/**
 * The settle decision is what keeps an untouched chat out of the way.
 *
 * That was not load-bearing while Terminal.vue's restore skipped every chat
 * with no session id and no messages — a filter that could not tell a
 * brand-new chat from an abandoned one, so it hid both: every chat the user
 * created and did not immediately talk to (until the next restart, then gone
 * for good), and every chat created from the phone, which has neither by
 * definition. With that filter removed, an empty chat shows up empty and this
 * function is the only thing deciding whether it sits in the active list or
 * ages onto the settled shelf.
 */
import { describe, it, expect } from "vitest";
import { AUTO_SETTLE_AFTER_DAYS, settledFor } from "./claudeChats";
import type { ClaudeSession } from "./claudeChats";
import { childrenOf, topLevel, allChildrenOf } from "./chatTree";

const DAY = 24 * 60 * 60 * 1000;
const NOW = 1_800_000_000_000;

const chat = (over: Partial<ClaudeSession> = {}): ClaudeSession =>
  ({
    id: 1,
    workspaceId: 2,
    title: "Chat 1",
    busy: false,
    status: "idle",
    transport: "claude-cli",
    claudeSessionId: "",
    messageCount: 0,
    lastActivityAt: NOW,
    ...over,
  }) as ClaudeSession;

describe("settledFor", () => {
  it("keeps a freshly created empty chat in the active list", () => {
    // The phone's case: no session id, no messages, created seconds ago. This
    // is the one that was invisible.
    expect(settledFor(chat({ lastActivityAt: NOW - 1_000 }), NOW)).toBe(false);
  });

  it("settles an untouched chat once it is old enough", () => {
    expect(settledFor(chat({ lastActivityAt: NOW - AUTO_SETTLE_AFTER_DAYS * DAY }), NOW)).toBe(true);
    expect(settledFor(chat({ lastActivityAt: NOW - (AUTO_SETTLE_AFTER_DAYS * DAY - 1) }), NOW)).toBe(false);
  });

  it("settles a chat that has no timestamp at all", () => {
    // Rows migrated from older builds carry lastActivityAt 0. Treating the
    // missing value as "epoch" is what puts them on the shelf instead of at
    // the top of the list.
    expect(settledFor(chat({ lastActivityAt: 0 }), NOW)).toBe(true);
    expect(settledFor(chat({ lastActivityAt: undefined }), NOW)).toBe(true);
  });

  it("never settles a chat with work pending, however old", () => {
    const ancient = { lastActivityAt: 0 };
    for (const status of ["running", "waiting", "permission"] as const) {
      expect(settledFor(chat({ ...ancient, status }), NOW)).toBe(false);
    }
    expect(settledFor(chat({ ...ancient, busy: true }), NOW)).toBe(false);
  });

  it("lets a manual pin win over the clock in both directions", () => {
    expect(settledFor(chat({ lastActivityAt: NOW, settledOverride: "settled" }), NOW)).toBe(true);
    expect(settledFor(chat({ lastActivityAt: 0, settledOverride: "active" }), NOW)).toBe(false);
  });

  it("does not settle a chat that does not exist", () => {
    // Callers look a session up by id and may miss; `undefined` must not read
    // as "settled" and hide a row whose data simply has not arrived yet.
    expect(settledFor(undefined, NOW)).toBe(false);
  });
});

const treeSessions = [
  { id: 1, workspaceId: 1, parentChatId: undefined },
  { id: 2, workspaceId: 1, parentChatId: 1 },
  { id: 3, workspaceId: 1, parentChatId: 1 },
  { id: 4, workspaceId: 1, parentChatId: undefined },
  { id: 5, workspaceId: 2, parentChatId: 4 },
] as any[];

describe("chat tree", () => {
  it("lists a thread's children in id order", () => {
    expect(childrenOf(treeSessions, 1).map((s) => s.id)).toEqual([2, 3]);
  });

  it("keeps sub-agents out of the top-level list", () => {
    // This is what stops a thread that spawned three helpers from showing four
    // sibling entries in the Sidebar.
    expect(topLevel(treeSessions, 1).map((s) => s.id)).toEqual([1, 4]);
  });

  it("treats an unknown parent as no children", () => {
    expect(childrenOf(treeSessions, 99)).toEqual([]);
  });

  it("drops an archived child — archive() already stopped its process", () => {
    // MINOR fix: archive(parent) stops each child's CLI (claudeChats.ts), so
    // a still-listed archived child is a dead entry that would restart its
    // process the moment something mounts it again (SubAgentHost.vue).
    const withArchived = [
      { id: 1, workspaceId: 1, parentChatId: undefined },
      { id: 2, workspaceId: 1, parentChatId: 1 },
      { id: 3, workspaceId: 1, parentChatId: 1, archivedAt: 12345 },
    ] as any[];
    expect(childrenOf(withArchived, 1).map((s) => s.id)).toEqual([2]);
  });

  it("allChildrenOf includes archived children — teardown must still reach them", () => {
    // Regression fix: childrenOf() gained the archivedAt filter above for
    // DISPLAY, but remove()/archive()'s own recursion (claudeChats.ts) used
    // to run on the same function — so removing a parent skipped an
    // already-archived child, leaving its session/actor/chatSession alive on
    // the frontend after Go had already deleted its row. allChildrenOf is
    // the teardown-only variant that does not filter archivedAt.
    const withArchived = [
      { id: 1, workspaceId: 1, parentChatId: undefined },
      { id: 2, workspaceId: 1, parentChatId: 1 },
      { id: 3, workspaceId: 1, parentChatId: 1, archivedAt: 12345 },
    ] as any[];
    expect(allChildrenOf(withArchived, 1).map((s) => s.id)).toEqual([2, 3]);
  });
});
