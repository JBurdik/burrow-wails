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
import { settledFor, isActivitySync } from "./claudeChats";
import type { ClaudeSession } from "./claudeChats";
import { childrenOf, topLevel, allChildrenOf } from "./chatTree";

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

// The rule itself is Go's (src-wails/chatattention_test.go has the ported
// cases); the client only compares settledSince with its clock and gates on
// work in flight.
describe("settledFor", () => {
  it("is settled from settledSince on, never before", () => {
    expect(settledFor(chat({ settledSince: NOW }), NOW)).toBe(true);
    expect(settledFor(chat({ settledSince: NOW + 1 }), NOW)).toBe(false);
    expect(settledFor(chat({ settledSince: 1 }), NOW)).toBe(true);
  });

  it("is not settled when Go says never (0) or nothing yet", () => {
    expect(settledFor(chat({ settledSince: 0 }), NOW)).toBe(false);
    expect(settledFor(chat({ settledSince: undefined }), NOW)).toBe(false);
  });

  it("never settles a chat with work pending", () => {
    for (const status of ["running", "waiting", "permission"] as const) {
      expect(settledFor(chat({ settledSince: 1, status }), NOW)).toBe(false);
    }
    expect(settledFor(chat({ settledSince: 1, busy: true }), NOW)).toBe(false);
  });

  it("does not settle a chat that does not exist", () => {
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

describe("isActivitySync", () => {
  it("counts a turn starting and a new message, not restore bookkeeping", () => {
    const prev = { status: "idle", messageCount: 3, busy: false } as const;
    expect(isActivitySync(prev, { status: "running" })).toBe(true);
    expect(isActivitySync(prev, { messageCount: 4, busy: true })).toBe(true);
    // Opening an idle chat folds what it streamed unwatched — not activity.
    expect(isActivitySync(prev, { messageCount: 9, busy: false })).toBe(false);
    // What a restart replays for every chat — must not restamp the thread.
    expect(isActivitySync(prev, { claudeSessionId: "abc" } as never)).toBe(false);
    expect(isActivitySync(prev, { title: "Renamed" } as never)).toBe(false);
    expect(isActivitySync(prev, { status: "idle", messageCount: 3 })).toBe(false);
    expect(isActivitySync({ status: "running", messageCount: 3, busy: true }, { status: "running" })).toBe(false);
  });
});
