import { describe, expect, it } from "vitest";
import { applyChatEvent, type ChatProjectionState } from "./chatProjection";
import type { ChatMessage } from "./chatTypes";
import { latestTodos } from "./chatTodos";

const todoWrite = (id: number, todos: unknown): ChatMessage => ({
  id, role: "tool", text: "TodoWrite", toolRawName: true, toolInput: { todos },
});

describe("latestTodos", () => {
  it("reads the most recent TodoWrite call and normalises its items", () => {
    const messages: ChatMessage[] = [
      todoWrite(1, [{ content: "old", status: "pending" }]),
      { id: 2, role: "assistant", text: "working" },
      { id: 3, role: "tool", text: "Bash", toolRawName: true, toolInput: { command: "ls" } },
      todoWrite(4, [
        { content: "Write helper", status: "completed", activeForm: "Writing helper" },
        { content: "Add surface", status: "in_progress", activeForm: "Adding surface" },
        { content: "Test", status: "weird" },
        { status: "pending" },
      ]),
    ];
    expect(latestTodos(messages)).toEqual([
      { content: "Write helper", status: "completed", activeForm: "Writing helper" },
      { content: "Add surface", status: "in_progress", activeForm: "Adding surface" },
      { content: "Test", status: "pending" },
    ]);
  });

  it("returns [] with no TodoWrite, and ignores an ACP title that happens to match", () => {
    expect(latestTodos([{ id: 1, role: "user", text: "hi" }])).toEqual([]);
    expect(latestTodos([{ id: 1, role: "tool", text: "TodoWrite", toolInput: { todos: [{ content: "x", status: "pending" }] } }])).toEqual([]);
  });

  it("reads a Codex plan, which Go projects as a TodoWrite row (acpPlanEvents)", () => {
    // What NormalizeAcpLine emits for Codex `turn/plan/updated` → ACP `plan`.
    const state: ChatProjectionState = { messages: [], nextMsgId: 1 };
    const plan = (todos: unknown[]) => {
      applyChatEvent(state, { type: "tool.started", toolCallId: "acp-plan", name: "TodoWrite", input: { todos } });
      applyChatEvent(state, { type: "tool.completed", toolCallId: "acp-plan" });
    };
    plan([{ content: "Read code", status: "in_progress" }, { content: "Fix", status: "pending" }]);
    applyChatEvent(state, { type: "text.delta", messageId: "acp:m1", text: "working" });
    plan([{ content: "Read code", status: "completed" }, { content: "Fix", status: "in_progress" }]);
    expect(state.messages.filter((m) => m.text === "TodoWrite").every((m) => m.toolOutput === "")).toBe(true);
    expect(latestTodos(state.messages)).toEqual([
      { content: "Read code", status: "completed" },
      { content: "Fix", status: "in_progress" },
    ]);
  });
});
