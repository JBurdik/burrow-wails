import { describe, it, expect } from "vitest";
import { applyChatEvent, isProjectedEvent, settleTranscript, type ChatProjectionState } from "./chatProjection";

function state(): ChatProjectionState {
  return { messages: [], nextMsgId: 0 };
}

describe("transcript projection", () => {
  it("appends native deltas into one bubble, by position", () => {
    const s = state();
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "Hel" });
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "lo" });
    expect(s.messages).toHaveLength(1);
    expect(s.messages[0]).toMatchObject({ role: "assistant", text: "Hello", partial: true });
  });

  it("starts a new bubble once something else came in between", () => {
    const s = state();
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "a" });
    applyChatEvent(s, { type: "tool.started", toolCallId: "t1", name: "Bash", input: { command: "pwd" } });
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "b" });
    expect(s.messages.map((m) => m.role)).toEqual(["assistant", "tool", "assistant"]);
    expect(s.messages[2].text).toBe("b");
  });

  it("matches ACP messages by id, not position — two can interleave", () => {
    const s = state();
    applyChatEvent(s, { type: "text.delta", messageId: "acp:m1", text: "one " });
    applyChatEvent(s, { type: "text.delta", messageId: "acp:m2", text: "two " });
    applyChatEvent(s, { type: "text.delta", messageId: "acp:m1", text: "more" });
    expect(s.messages).toHaveLength(2);
    expect(s.messages[0].text).toBe("one more");
    expect(s.messages[1].text).toBe("two ");
  });

  it("gives every message a distinct id", () => {
    const s = state();
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "a" });
    applyChatEvent(s, { type: "tool.started", toolCallId: "t1", name: "Bash" });
    applyChatEvent(s, { type: "thinking.delta", text: "hmm" });
    const ids = s.messages.map((m) => m.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("routes a tool result to the LAST call with that id", () => {
    const s = state();
    applyChatEvent(s, { type: "tool.started", toolCallId: "t1", name: "Bash", input: {} });
    applyChatEvent(s, { type: "tool.started", toolCallId: "t2", name: "Bash", input: {} });
    applyChatEvent(s, { type: "tool.completed", toolCallId: "t1", output: "first" });
    applyChatEvent(s, { type: "tool.completed", toolCallId: "t2", output: "boom", failed: true });
    expect(s.messages[0]).toMatchObject({ toolUseId: "t1", toolOutput: "first", toolFailed: false });
    expect(s.messages[1]).toMatchObject({ toolUseId: "t2", toolOutput: "boom", toolFailed: true });
  });

  it("ignores a result for a call it never saw", () => {
    const s = state();
    expect(applyChatEvent(s, { type: "tool.completed", toolCallId: "ghost", output: "x" })).toBe(false);
    expect(s.messages).toHaveLength(0);
  });

  it("marks native tool names raw and ACP titles not", () => {
    const s = state();
    applyChatEvent(s, { type: "tool.started", toolCallId: "t1", name: "Bash", input: { command: "pwd" } });
    applyChatEvent(s, { type: "tool.started", toolCallId: "t2", name: "Read file" });
    expect(s.messages[0].toolRawName).toBe(true);
    expect(s.messages[1].toolRawName).toBe(false);
  });

  it("renders a replayed user turn as a finished bubble", () => {
    const s = state();
    applyChatEvent(s, { type: "user.delta", messageId: "acp:u1", text: "do " });
    applyChatEvent(s, { type: "user.delta", messageId: "acp:u1", text: "it" });
    expect(s.messages).toHaveLength(1);
    expect(s.messages[0]).toMatchObject({ role: "user", text: "do it" });
    expect(s.messages[0].partial).toBeUndefined();
  });

  it("drops empty chunks instead of creating empty bubbles", () => {
    const s = state();
    expect(applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "" })).toBe(false);
    expect(s.messages).toHaveLength(0);
  });

  it("settling un-partials everything, not just the last message", () => {
    const s = state();
    // Tools are pushed after assistant text, so checking only the tail would
    // leave the text bubble streaming forever.
    applyChatEvent(s, { type: "text.delta", messageId: "c1", text: "a" });
    applyChatEvent(s, { type: "thinking.delta", text: "b" });
    settleTranscript(s);
    expect(s.messages.every((m) => !m.partial)).toBe(true);
  });

  it("knows which events it renders", () => {
    expect(isProjectedEvent("text.delta")).toBe(true);
    expect(isProjectedEvent("tool.completed")).toBe(true);
    // The view acts on these; the transcript does not show them.
    expect(isProjectedEvent("turn.completed")).toBe(false);
    expect(isProjectedEvent("session.exited")).toBe(false);
  });
});
