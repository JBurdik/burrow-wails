/**
 * Regression coverage: `childrenOf` gained an `archivedAt` filter (for
 * display — see chatTree.ts), but `remove()`/`archive()`'s own recursion in
 * claudeChats.ts used to call THAT same function. So `remove(parent)` (and
 * `archive(parent)`) skipped an already-archived child entirely: Go still
 * deleted its row (`delete_chat` cascades on parent_chat_id, per chats.go),
 * but the frontend kept a stale `sessions` entry, its actor, and its
 * `chatSession` listeners alive until the next reload.
 *
 * The fix: teardown recursion uses `allChildrenOf` (unfiltered by
 * archivedAt), while display paths (SubAgentHost.vue, RightPanel.vue) keep
 * using `childrenOf`. This test proves `remove(parent)` reaches BOTH a live
 * and an already-archived child.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

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

const deletedChatIds: number[] = [];

vi.mock("@tauri-apps/api/core", () => ({
  invoke: async (cmd: string, args: Record<string, unknown>) => {
    if (cmd === "delete_chat") {
      deletedChatIds.push(args.id as number);
      return null;
    }
    if (cmd === "list_chats") return [];
    if (cmd === "read_config") return "{}";
    // claude_stop/codex_stop/acp_stop and anything else: no-op success.
    return null;
  },
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

describe("remove() tears down every child, archived included", () => {
  beforeEach(() => {
    deletedChatIds.length = 0;
    setActivePinia(createPinia());
  });

  it("removes a live child and an already-archived child alike", async () => {
    const { useClaudeChatsStore } = await import("./claudeChats");
    const chats = useClaudeChatsStore();

    // The store's own construction fires a fire-and-forget `void reload()`
    // (list_chats → sessions.value = [...]). Left unawaited, its continuation
    // can interleave with remove()'s own several `await`s below and wipe
    // `sessions.value` mid-teardown — a test-harness race, not a store bug.
    // Flushing it out here (it resolves to [] per the mock above) before
    // patching in test data is what keeps remove()'s own awaits from
    // racing it.
    await Promise.resolve();
    await Promise.resolve();

    chats.$patch((state) => {
      state.sessions.push(
        { id: 1, workspaceId: 1, title: "parent", busy: false, claudeSessionId: "", messageCount: 0, transport: "claude-cli" } as any,
        { id: 2, workspaceId: 1, title: "live child", busy: false, claudeSessionId: "", messageCount: 0, transport: "claude-cli", parentChatId: 1 } as any,
        {
          id: 3,
          workspaceId: 1,
          title: "archived child",
          busy: false,
          claudeSessionId: "",
          messageCount: 0,
          transport: "claude-cli",
          parentChatId: 1,
          archivedAt: 12345,
        } as any,
      );
    });

    await chats.remove(1);

    // Go's delete_chat must have been called for the parent AND both
    // children — the archived one included. Before the fix, id 3 never got
    // here at all.
    expect(deletedChatIds.sort()).toEqual([1, 2, 3]);
    // And the frontend's own session list must not keep a stale entry for
    // any of them — the actual symptom (a dangling session/actor/
    // chatSession the recursion skipped).
    expect(chats.sessions.map((s) => s.id)).toEqual([]);
  });
});
