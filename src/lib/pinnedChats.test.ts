import { describe, it, expect, beforeEach, vi } from "vitest";

const store: Record<string, unknown> = {};
vi.mock("./config", () => ({
  configReady: Promise.resolve(),
  getConfig: <T>(key: string, fallback: T): T => (store[key] as T) ?? fallback,
  setConfig: (key: string, value: unknown) => {
    store[key] = value;
  },
}));

const { pinnedChatIds, isChatPinned, toggleChatPin, unpinChat } = await import("./pinnedChats");

describe("pinnedChats", () => {
  beforeEach(() => {
    pinnedChatIds.value = [];
    delete store.sidebarPinnedChats;
  });

  it("toggles a pin on and off", () => {
    toggleChatPin(7);
    expect(isChatPinned(7)).toBe(true);
    toggleChatPin(7);
    expect(isChatPinned(7)).toBe(false);
  });

  it("persists through config so a restart restores the pins", () => {
    toggleChatPin(7);
    toggleChatPin(9);
    expect(store.sidebarPinnedChats).toEqual([7, 9]);
  });

  it("treats an undefined chat id as unpinned", () => {
    expect(isChatPinned(undefined)).toBe(false);
  });

  it("unpinChat is a no-op for a thread that was never pinned", () => {
    unpinChat(4);
    expect(pinnedChatIds.value).toEqual([]);
  });
});
