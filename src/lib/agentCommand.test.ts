import { describe, it, expect } from "vitest";
import { buildTerminalCommand, shellQuote } from "./agentCommand";

describe("shellQuote", () => {
  it("wraps plain text", () => {
    expect(shellQuote("fix the bug")).toBe("'fix the bug'");
  });

  it("survives an embedded quote", () => {
    // The classic break-out: without the '\'' dance this ends the string early.
    expect(shellQuote("don't; rm -rf /")).toBe(`'don'\\''t; rm -rf /'`);
  });
});

describe("buildTerminalCommand", () => {
  it("passes model and permission mode for Claude", () => {
    expect(buildTerminalCommand({ kind: "claude", command: "claude", model: "opus", permMode: "plan" }, "hi"))
      .toBe("claude --model opus --permission-mode plan 'hi'");
  });

  it("maps bypass to the skip-permissions flag", () => {
    expect(buildTerminalCommand({ kind: "claude", command: "claude", permMode: "bypassPermissions" }, "hi"))
      .toBe("claude --dangerously-skip-permissions 'hi'");
  });

  it("drops chat-only permission modes", () => {
    expect(buildTerminalCommand({ kind: "claude", command: "claude", permMode: "dontAsk" }, "hi")).toBe("claude 'hi'");
  });

  it("uses the interactive CLI, not the adapter command", () => {
    expect(buildTerminalCommand({ kind: "codex", command: "codex app-server", model: "gpt" }, "hi")).toBe("codex 'hi'");
  });

  it("falls back to the bare program for custom agents", () => {
    expect(buildTerminalCommand({ kind: "custom", command: "opencode" }, "hi")).toBe("opencode 'hi'");
  });
});
