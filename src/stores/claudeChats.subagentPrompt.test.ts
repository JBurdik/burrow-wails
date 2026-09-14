/**
 * Regression coverage for the sub-agent spawn ordering bug: `create()` used
 * to push the new session into `sessions.value` and only AFTER that let the
 * caller (`controlBridge.ts`'s `spawn`) register the initial prompt in a
 * side-table. `push` schedules Vue's reactive effects as a microtask job
 * synchronously, before `create()` even returns — so a watcher reacting to
 * `sessions.value` (SubAgentHost.vue, in the real app) could run and read
 * "no prompt queued yet" before the caller's `await chats.create(...)`
 * continuation got a turn to fill it in. The queued prompt was silently
 * dropped and the sub-agent's CLI never started.
 *
 * The fix moves prompt registration INTO `create()`, before the push. This
 * test does not need a mounted component: it just asserts the prompt is
 * retrievable for the new session's id the moment that id is observable in
 * `sessions.value` — which is the same guarantee SubAgentHost's watcher
 * depends on.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

// claudeChats.ts's store setup does a fire-and-forget
// `migrateFromLocalStorage` once `configReady` resolves; this suite runs
// with no DOM env (see CLAUDE.md), so `localStorage` doesn't exist unless a
// test provides one. Minimal stub — nothing here reads or writes real keys.
if (typeof globalThis.localStorage === "undefined") {
  (globalThis as unknown as { localStorage: Storage }).localStorage = {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
    clear: () => {},
    key: () => null,
    length: 0,
  } as Storage;
}

let nextRowId = 100;

vi.mock("@tauri-apps/api/core", () => ({
  invoke: async (cmd: string, args: Record<string, unknown>) => {
    if (cmd === "create_chat") {
      const chat = args.chat as Record<string, unknown>;
      return { ...chat, id: nextRowId++ };
    }
    if (cmd === "list_chats") return [];
    if (cmd === "read_config") return "{}";
    return null;
  },
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

describe("sub-agent prompt handoff is race-free", () => {
  beforeEach(() => {
    nextRowId = 100;
    setActivePinia(createPinia());
  });

  it("has the prompt available for a child's id the instant it appears in sessions.value", async () => {
    const { useClaudeChatsStore, takePendingSubagentPrompt, isLocallyCreatedSubagent } = await import("./claudeChats");
    const chats = useClaudeChatsStore();

    // Emulate SubAgentHost's reactive watcher: it fires whenever
    // sessions.value changes, and would previously read the prompt queue
    // before create() had written to it.
    let observedAtPush: string | undefined;
    let observedIsLocal = false;
    const stop = chats.$subscribe((_mutation, state) => {
      const child = state.sessions.find((s: { parentChatId?: number }) => s.parentChatId === 42);
      if (child && observedAtPush === undefined) {
        observedIsLocal = isLocallyCreatedSubagent(child.id);
        observedAtPush = takePendingSubagentPrompt(child.id);
      }
    });

    const session = await chats.create(1, {
      agentKind: "claude",
      parentChatId: 42,
      initialPrompt: "investigate the flaky test",
    });
    stop();

    expect(observedIsLocal).toBe(true);
    expect(observedAtPush).toBe("investigate the flaky test");
    expect(session.parentChatId).toBe(42);
  });

  it("treats a session never locally created as absent-by-design, not a race", async () => {
    const { useClaudeChatsStore, takePendingSubagentPrompt, isLocallyCreatedSubagent } = await import("./claudeChats");
    const chats = useClaudeChatsStore();

    // A child that arrived via reload()/list_chats (another client, or this
    // one after a restart) rather than this process's own create().
    chats.$patch((state) => {
      state.sessions.push({
        id: 999,
        workspaceId: 1,
        title: "Chat",
        busy: false,
        claudeSessionId: "",
        messageCount: 0,
        parentChatId: 42,
      } as any);
    });

    expect(isLocallyCreatedSubagent(999)).toBe(false);
    expect(takePendingSubagentPrompt(999)).toBeUndefined();
  });
});
