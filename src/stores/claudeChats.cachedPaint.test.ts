/**
 * A paired device paints the chat list from its read cache (`cachedRead`)
 * before `list_chats` answers. Those rows can be days old, and `persist()`
 * upserts EVERY held row (title, archived_at, claude_session_id, …) through
 * save_chats — so a persist in that window (setActive from a deep link at cold
 * boot) wrote the stale picture back over Go's truth, e.g. un-archiving chats.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

if (typeof globalThis.localStorage === "undefined") {
  (globalThis as unknown as { localStorage: Storage }).localStorage = {
    getItem: () => null, setItem: () => {}, removeItem: () => {}, clear: () => {}, key: () => null, length: 0,
  } as Storage;
}

const h = vi.hoisted(() => {
  let release: (rows: unknown[]) => void = () => {};
  const listChats = new Promise<unknown[]>((r) => { release = r; });
  const calls: Array<{ cmd: string; args: any }> = [];
  return { listChats, release: (rows: unknown[]) => release(rows), calls };
});

vi.mock("@tauri-apps/api/core", () => ({
  cachedRead: (cmd: string) =>
    cmd === "list_chats" ? [{ id: 7, workspace_id: 1, title: "stale", archived_at: 0 }] : null,
  invoke: async (cmd: string, args: unknown) => {
    h.calls.push({ cmd, args });
    if (cmd === "list_chats") return h.listChats;
    if (cmd === "read_config") return "{}";
    return null;
  },
}));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

describe("claudeChats cached first paint", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("never saves the cached rows; saves once the live list landed", async () => {
    const { useClaudeChatsStore } = await import("./claudeChats");
    const chats = useClaudeChatsStore();
    expect(chats.sessions.map((s) => s.title)).toEqual(["stale"]);

    chats.setActive(1, 7);
    expect(h.calls.some((c) => c.cmd === "save_chats")).toBe(false);

    h.release([{ id: 7, workspace_id: 1, title: "live", archived_at: 123 }]);
    await vi.waitFor(() => expect(chats.sessions[0]?.title).toBe("live"));
    chats.setActive(1, 7);
    const save = h.calls.find((c) => c.cmd === "save_chats");
    expect(save?.args.chats[0]).toMatchObject({ title: "live" });
  });
});
