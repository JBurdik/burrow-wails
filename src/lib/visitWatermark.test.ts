import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@/lib/chatTypes";
import { visitWatermark } from "@/lib/visitWatermark";

const msg = (id: number, role: ChatMessage["role"], at?: number): ChatMessage => ({ id, role, text: "", ...(at ? { at } : {}) });

describe("visitWatermark", () => {
  const read = [msg(1, "user", 100), msg(2, "assistant", 110)];

  it("marks the first message after the receipt once two agent rows arrived", () => {
    expect(visitWatermark([...read, msg(3, "thinking", 300), msg(4, "tool", 310), msg(5, "assistant", 320)], 200)).toBe(3);
  });

  it("skips a single new reply", () => {
    expect(visitWatermark([...read, msg(3, "thinking", 300), msg(4, "assistant", 310)], 200)).toBeNull();
  });

  it("shows nothing when there is no receipt, nothing new, or everything is new", () => {
    const later = [...read, msg(3, "assistant", 300), msg(4, "assistant", 310)];
    expect(visitWatermark(later, null)).toBeNull();
    expect(visitWatermark(later, 400)).toBeNull();
    expect(visitWatermark(later, 50)).toBeNull();
  });

  it("treats unstamped rows as read and ignores queued ones", () => {
    const rows = [msg(1, "user"), msg(2, "assistant"), msg(3, "queued", 300), msg(4, "assistant", 310), msg(5, "tool", 320)];
    expect(visitWatermark(rows, 200)).toBe(4);
  });
});
