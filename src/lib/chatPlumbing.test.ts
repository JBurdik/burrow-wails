import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@/lib/chatTypes";
import { isPlumbingTool } from "./chatPlumbing";

const tool = (text: string, over: Partial<ChatMessage> = {}) => ({ id: 1, role: "tool", text, toolRawName: true, toolOutput: "ok", ...over }) as ChatMessage;

describe("isPlumbingTool", () => {
  it("hides a taken end_turn and ToolSearch", () => {
    expect(isPlumbingTool(tool("mcp__burrow__end_turn"))).toBe(true);
    expect(isPlumbingTool(tool("ToolSearch"))).toBe(true);
  });
  it("hides Codex/ACP's titled end_turn too", () => {
    expect(isPlumbingTool(tool("burrow: end_turn", { toolRawName: false }))).toBe(true);
    expect(isPlumbingTool(tool("burrow: pin", { toolRawName: false }))).toBe(false);
  });
  it("keeps real work, failures and calls still running", () => {
    expect(isPlumbingTool(tool("Bash"))).toBe(false);
    expect(isPlumbingTool(tool("mcp__burrow__pin"))).toBe(false);
    expect(isPlumbingTool(tool("mcp__burrow__end_turn", { toolFailed: true }))).toBe(false);
    expect(isPlumbingTool(tool("mcp__burrow__end_turn", { toolOutput: undefined }))).toBe(false);
  });
});
