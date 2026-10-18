import { describe, expect, it } from "vitest";
import { binaryFor, commandLine, newInstance, providerFor, seedInstances } from "./providers";
import { assertProfileProvider, profileTerminalCommand } from "./subagentProfiles";

describe("OpenAI compatible coding provider", () => {
  it("offers a built-in chat agent with independent connection settings", () => {
    const first = newInstance("openai-compatible", { id: "local" });
    const second = newInstance("openai-compatible", { id: "remote" });
    expect(first.kind).toBe("custom");
    expect(first.transport).toBe("acp");
    expect(first.openai?.baseUrl).toBe("http://127.0.0.1:8080/v1");
    expect(first.openai?.contextWindow).toBe(32768);
    first.openai!.apiKey = "local-only";
    expect(second.openai?.apiKey).toBe("");
    expect(providerFor(first.providerId).npmPackage).toBeUndefined();
    expect(binaryFor(first)).toBe("");
    expect(commandLine({ ...first, terminalArgs: "--model anything" })).toBe("");
  });
  it("supports role profiles as chats and rejects a nonexistent terminal CLI", () => {
    const provider = newInstance("openai-compatible");
    expect(() => assertProfileProvider(provider)).not.toThrow();
    expect(() => assertProfileProvider({ ...provider, enabled: false })).toThrow();
    expect(() => profileTerminalCommand(provider, { agentId: provider.id, model: "coder", permissionMode: "plan" }, "Inspect")).toThrow("run as chats");
    expect(seedInstances().map((p) => p.id)).toEqual(["claude", "codex", "gemini", "opencode"]);
  });
});
