import { describe, it, expect } from "vitest";
import { activeChatIdFor } from "./chatTree";

const tabsByWs = {
  1: [
    { id: 10, chatId: 120 },
    { id: 11 }, // a terminal tab, no chat
    { id: 12, chatId: 130 },
  ],
  2: [{ id: 20, chatId: 200 }],
};

describe("activeChatIdFor", () => {
  it("reports the chat of the active tab", () => {
    expect(activeChatIdFor(tabsByWs, { 1: 12 }, 1)).toBe(130);
  });

  it("is null when the active tab is a terminal, not a chat", () => {
    // The panel must say "no thread on screen" rather than showing the
    // sub-agents of whatever chat happened to be open before.
    expect(activeChatIdFor(tabsByWs, { 1: 11 }, 1)).toBeNull();
  });

  it("is per workspace", () => {
    expect(activeChatIdFor(tabsByWs, { 1: 10, 2: 20 }, 2)).toBe(200);
  });

  it("is null with no active tab, an unknown tab, or no workspace", () => {
    expect(activeChatIdFor(tabsByWs, {}, 1)).toBeNull();
    expect(activeChatIdFor(tabsByWs, { 1: 999 }, 1)).toBeNull();
    expect(activeChatIdFor(tabsByWs, { 1: 10 }, undefined)).toBeNull();
  });
});
