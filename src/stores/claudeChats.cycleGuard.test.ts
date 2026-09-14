/**
 * MINOR fix: `parent_chat_id` rides SaveChats' last-writer-wins path
 * (chats.go), so two clients racing writes to it can leave a genuine cycle on
 * disk — chat A's parent is chat B, chat B's parent is chat A. remove()/
 * archive() recurse over childrenOf(), and with no cycle guard that recursion
 * never terminates. This test constructs exactly that cycle and asserts both
 * functions return instead of blowing the stack.
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

vi.mock("@tauri-apps/api/core", () => ({
  invoke: async (cmd: string) => {
    if (cmd === "list_chats") return [];
    if (cmd === "read_config") return "{}";
    return null;
  },
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

function makeCycle() {
  return [
    { id: 1, workspaceId: 1, title: "A", busy: false, claudeSessionId: "", messageCount: 0, transport: "claude-cli", parentChatId: 2 },
    { id: 2, workspaceId: 1, title: "B", busy: false, claudeSessionId: "", messageCount: 0, transport: "claude-cli", parentChatId: 1 },
  ] as any[];
}

describe("claudeChats cycle guard", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("remove() terminates on a parent_chat_id cycle", async () => {
    const { useClaudeChatsStore } = await import("./claudeChats");
    const chats = useClaudeChatsStore();
    chats.$patch((state) => {
      state.sessions.push(...makeCycle());
    });

    await expect(chats.remove(1)).resolves.toBeUndefined();
  });

  it("archive() terminates on a parent_chat_id cycle", async () => {
    const { useClaudeChatsStore } = await import("./claudeChats");
    const chats = useClaudeChatsStore();
    chats.$patch((state) => {
      state.sessions.push(...makeCycle());
    });

    await expect(chats.archive(1)).resolves.toBeUndefined();
  });
});
