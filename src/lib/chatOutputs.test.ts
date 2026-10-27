import { describe, expect, it } from "vitest";
import { chatOutputs } from "./chatOutputs";
import { applyChatEvent, type ChatProjectionState } from "./chatProjection";
import type { ChatMessage } from "./chatTypes";

let id = 0;
const tool = (name: string, toolInput: Record<string, unknown>, extra: Partial<ChatMessage> = {}): ChatMessage =>
  ({ id: id++, role: "tool", text: name, toolInput, toolRawName: true, ...extra });

describe("chatOutputs", () => {
  it("dedupes by path, newest first, keeps created over later edits, skips reads and failures", () => {
    const out = chatOutputs([
      tool("Write", { file_path: "/r/a.ts", content: "x" }),
      tool("Edit", { file_path: "/r/b.ts", old_string: "1", new_string: "2" }),
      tool("Read", { file_path: "/r/c.ts" }),
      tool("Edit", { file_path: "/r/a.ts", old_string: "x", new_string: "y" }),
      tool("Edit", { file_path: "/r/d.ts", old_string: "1", new_string: "2" }, { toolFailed: true }),
      tool("Apply file changes", { changes: [{ path: "/r/e.ts", kind: { type: "add" } }, { path: "/r/b.ts", kind: "update" }] }, { toolRawName: false }),
    ]);
    expect(out).toEqual([
      { path: "/r/b.ts", op: "edited", edits: 2 },
      { path: "/r/e.ts", op: "created", edits: 1 },
      { path: "/r/a.ts", op: "created", edits: 2 },
    ]);
  });

  it("reads Codex fileChange rows as projected from real tool.started events", () => {
    // Inputs captured from chat_stream (Codex app-server 0.164, via pumpCodexLine):
    // kind is an object; move_path is null for a plain edit.
    const state: ChatProjectionState = { messages: [], nextMsgId: 1 };
    const started = (toolCallId: string, changes: unknown[]) =>
      applyChatEvent(state, { type: "tool.started", toolCallId, name: "Apply file changes", input: { changes } });
    const done = (toolCallId: string, failed = false) =>
      applyChatEvent(state, { type: "tool.completed", toolCallId, output: "{}", failed });
    started("exec-1", [
      { diff: "S3_BUCKET=natalies-media\n", kind: { type: "add" }, path: "/Users/jirkab/code/natalies/.env.example" },
      { diff: ".env\n", kind: { type: "add" }, path: "/Users/jirkab/code/natalies/.gitignore" },
    ]);
    done("exec-1");
    started("exec-2", [
      { diff: "@@ -18,3 +18,3 @@\n", kind: { move_path: null, type: "update" }, path: "/Users/jirkab/code/natalies/compose.yml" },
    ]);
    done("exec-2");
    started("exec-3", [
      { diff: "@@ -2,3 +2,3 @@\n", kind: { move_path: null, type: "update" }, path: "/Users/jirkab/code/natalies/.env.example" },
    ]);
    done("exec-3");
    // Rename: the file is listed at its new path and keeps its history.
    started("exec-4", [
      { diff: "", kind: { move_path: "/Users/jirkab/code/natalies/.env.sample", type: "update" }, path: "/Users/jirkab/code/natalies/.env.example" },
      { diff: "", kind: { type: "delete" }, path: "/Users/jirkab/code/natalies/old.txt" },
    ]);
    done("exec-4");
    // A declined patch is reported failed by Go and wrote nothing.
    started("exec-5", [{ diff: "", kind: { type: "add" }, path: "/Users/jirkab/code/natalies/nope.ts" }]);
    done("exec-5", true);

    expect(chatOutputs(state.messages)).toEqual([
      { path: "/Users/jirkab/code/natalies/old.txt", op: "deleted", edits: 1 },
      { path: "/Users/jirkab/code/natalies/.env.sample", op: "created", edits: 3 },
      { path: "/Users/jirkab/code/natalies/compose.yml", op: "edited", edits: 1 },
      { path: "/Users/jirkab/code/natalies/.gitignore", op: "created", edits: 1 },
    ]);
  });
});
