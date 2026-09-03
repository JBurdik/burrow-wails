import { describe, it, expect, vi, beforeEach } from "vitest";

const store: Record<string, unknown> = {};
vi.mock("@/lib/config", () => ({
  getConfig: <T,>(k: string, fb: T) => (k in store ? (store[k] as T) : fb),
  setConfig: (k: string, v: unknown) => { store[k] = v; },
}));

const { forgetChatSettings, chatSettingKey } = await import("./chatSettings");

beforeEach(() => { for (const k of Object.keys(store)) delete store[k]; });

describe("chatSettingKey", () => {
  it("scopes by modelKey when given", () => {
    expect(chatSettingKey(7)).toBe("7");
    expect(chatSettingKey(7, "burrow.manager.model")).toBe("burrow.manager.model:7");
  });
});

describe("forgetChatSettings", () => {
  it("drops bare and modelKey-scoped entries, keeps other chats", () => {
    store.chatModelByChat = { "7": "opus", "burrow.manager.model:7": "sonnet", "17": "haiku", "8": "opus" };
    store.chatEffortByChat = { "7": "high", "8": "low" };
    store.chatAcpSettings = { "7": { model: "gpt" }, "8": { model: "gpt" } };
    store.chatPermissionMode = {
      byChat: { "7": "plan", "8": "default" },
      dangerousByChat: { "7": true },
      last: "plan",
    };

    forgetChatSettings(7);

    // "17" must survive — endsWith(":7") must not match a longer id.
    expect(store.chatModelByChat).toEqual({ "17": "haiku", "8": "opus" });
    expect(store.chatEffortByChat).toEqual({ "8": "low" });
    expect(store.chatAcpSettings).toEqual({ "8": { model: "gpt" } });
    expect(store.chatPermissionMode).toEqual({
      byChat: { "8": "default" },
      dangerousByChat: {},
      last: "plan", // non-per-chat fields untouched
    });
  });

  it("is a no-op when the chat has no stored settings", () => {
    store.chatModelByChat = { "8": "opus" };
    forgetChatSettings(7);
    expect(store.chatModelByChat).toEqual({ "8": "opus" });
    expect("chatPermissionMode" in store).toBe(false);
  });
});
