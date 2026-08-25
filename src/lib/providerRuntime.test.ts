import { describe, expect, it } from "vitest";
import { isProviderRuntimeEvent, normalizeAcpRuntimeEvent, normalizeClaudeStreamEvent } from "./providerRuntime";

describe("provider runtime event boundary", () => {
  it("accepts the normalized streamed text event", () => {
    expect(isProviderRuntimeEvent({ type: "text.delta", messageId: "m1", text: "hello" })).toBe(true);
  });

  it("rejects malformed provider output before it reaches the renderer", () => {
    expect(isProviderRuntimeEvent({ type: "text.delta", text: "missing id" })).toBe(false);
  });

  it("normalizes equivalent Claude and ACP tool starts", () => {
    expect(normalizeClaudeStreamEvent({ type: "assistant", message: { id: "c1", content: [{ type: "tool_use", id: "t1", name: "Bash", input: { command: "pwd" } }] } })).toEqual([
      { type: "tool.started", toolCallId: "t1", name: "Bash", input: { command: "pwd" } },
    ]);
    expect(normalizeAcpRuntimeEvent({ kind: "tool_call", toolCallId: "t1", title: "Run command" })).toEqual([
      { type: "tool.started", toolCallId: "t1", name: "Run command" },
    ]);
  });
});
