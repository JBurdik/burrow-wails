import { describe, it, expect, vi, beforeEach } from "vitest";
import { newInstance } from "@/lib/providers";
const config = vi.hoisted(() => ({} as Record<string, any>));
vi.mock("@/lib/config", () => ({
  configReady: Promise.resolve(),
  getConfig: (key: string, fallback: unknown) => config[key] ?? fallback,
  setConfig: (key: string, value: unknown) => { config[key] = value; },
}));
import { defaultSubagentProfiles, normalizeSubagentProfiles, profileTask, profileTerminalCommand, seedSubagentSettings, restoreSubagentCodexSettings } from "./subagentProfiles";
beforeEach(() => { for (const key in config) delete config[key]; });

describe("sub-agent role profiles", () => {
  it("keeps safe defaults for older or malformed configurations", () => {
    const roles = normalizeSubagentProfiles([{ id: "scout", instructions: "Find the entry point", permissionMode: "bypassPermissions" }]);
    expect(roles).toHaveLength(3);
    expect(roles[0].permissionMode).toBe("plan");
    expect(roles[0].instructions).toBe("Find the entry point");
    expect(roles[1].permissionMode).toBe("default");
    expect(profileTask(roles[0], "Investigate login")).toContain("Find the entry point\n\nTask:\nInvestigate login");
  });
  it("seeds child settings without changing another chat or global defaults", async () => {
    config.chatPermissionMode = { byChat: { "7": "auto" }, last: "auto", dangerousByChat: {} };
    config.chatAcpLast = { codex: { mode: "full-access", model: "old" } };
    config.chatModelByChat = { "7": "old" };
    config.chatProfileSelection = { "7": "claude-personal" };
    seedSubagentSettings(42, { agentId: "codex", permissionMode: "plan", model: "chosen-model" });
    expect(config.chatPermissionMode.last).toBe("auto");
    expect(config.chatPermissionMode.byChat).toEqual({ "7": "auto", "42": "plan" });
    expect(config.chatAcpLast.codex.mode).toBe("full-access");
    expect(config.chatModelByChat).toEqual({ "7": "old", "42": "chosen-model" });
    expect(config.chatProfileSelection).toEqual({ "7": "claude-personal", "42": "codex" });
    const invoke = vi.fn().mockResolvedValue(1);
    await restoreSubagentCodexSettings(42, invoke);
    expect(invoke.mock.calls).toEqual([
      ["acp_set_mode", { id: 42, modeId: "plan" }],
      ["acp_set_config", { id: 42, configId: "model", value: "chosen-model" }],
    ]);
  });
  it("fails closed if permission restoration fails, before setting a model", async () => {
    seedSubagentSettings(42, { agentId: "codex", permissionMode: "plan", model: "chosen" });
    const invoke = vi.fn().mockRejectedValue(new Error("connection lost"));
    await expect(restoreSubagentCodexSettings(42, invoke)).rejects.toThrow("connection lost");
    expect(invoke).toHaveBeenCalledTimes(1);
  });
  it("passes native model and permissions to terminal profiles and quotes the task", () => {
    const settings = { agentId: "codex", permissionMode: "plan" as const, model: "model; echo secret" };
    const cmd = profileTerminalCommand(newInstance("codex"), settings, "Don't edit; $(touch /tmp/no)");
    expect(cmd).toContain("--sandbox read-only --ask-for-approval on-request");
    expect(cmd).toContain("--model 'model; echo secret'");
    expect(cmd).toContain("'Don'\\''t edit; $(touch /tmp/no)'");
    expect(profileTerminalCommand(newInstance("claude"), { ...settings, agentId: "claude" }, "inspect")).toContain("--permission-mode plan");
    expect(() => profileTerminalCommand(newInstance("gemini"), settings, "inspect")).toThrow("require an enabled Claude");
    expect(defaultSubagentProfiles().map((role) => role.id)).toEqual(["scout", "worker", "reviewer"]);
  });
  it("preserves provider environment and rejects conflicting terminal arguments", () => {
    const settings = { agentId: "claude-team", permissionMode: "plan" as const, model: "" };
    const provider = newInstance("claude", { id: "claude-team", configDir: "/tmp/team config", env: { TEAM: "a; echo no" } });
    const cmd = profileTerminalCommand(provider, settings, "inspect");
    expect(cmd).toContain("'TEAM=a; echo no'");
    expect(cmd).toContain("'CLAUDE_CONFIG_DIR=/tmp/team config'");
    expect(() => profileTerminalCommand({ ...provider, terminalArgs: "--dangerously-skip-permissions" }, settings, "inspect")).toThrow("custom provider arguments");
  });
});
