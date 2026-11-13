import { beforeEach, describe, expect, it, vi } from "vitest";

const store: Record<string, unknown> = {};
vi.mock("@/lib/config", () => ({
  getConfig: <T,>(key: string, fallback: T) => (key in store ? (store[key] as T) : fallback),
  setConfig: (key: string, value: unknown) => { store[key] = value; },
}));

const {
  getAcpCapabilities, getAcpChatSetting, getLastAcpSetting,
  setAcpCapabilities, setAcpChatSetting, setLastAcpSetting, rpcLaunchSeed,
} = await import("./acpSettings");

beforeEach(() => { for (const key of Object.keys(store)) delete store[key]; });

describe("ACP composer settings", () => {
  it("shares the last Codex model between welcome and a new chat", () => {
    setLastAcpSetting("codex", "model", "gpt-5.6-sol");
    expect(getLastAcpSetting("codex", "model")).toBe("gpt-5.6-sol");

    setAcpChatSetting(12, "codex", "effort", "high");
    expect(getAcpChatSetting(12, "effort")).toBe("high");
    expect(getLastAcpSetting("codex", "model")).toBe("gpt-5.6-sol");
    expect(getLastAcpSetting("codex", "effort")).toBe("high");
  });

  it("keeps independent settings per chat and provider", () => {
    setAcpChatSetting(12, "codex", "mode", "auto");
    setAcpChatSetting(13, "gemini", "mode", "read-only");

    expect(getAcpChatSetting(12, "mode")).toBe("auto");
    expect(getAcpChatSetting(13, "mode")).toBe("read-only");
    expect(getLastAcpSetting("codex", "mode")).toBe("auto");
    expect(getLastAcpSetting("gemini", "mode")).toBe("read-only");
  });

  it("keeps the selector catalogue across a chat remount", () => {
    setAcpCapabilities(12, {
      agentId: "codex",
      modes: { currentModeId: "auto", availableModes: [{ id: "auto", name: "Auto" }] },
      configOptions: [{
        id: "effort", name: "Effort", type: "select", currentValue: "high",
        options: [{ value: "high", name: "High" }],
      }],
    });

    expect(getAcpCapabilities(12, "codex")?.configOptions[0].currentValue).toBe("high");
    expect(getAcpCapabilities(12, "gemini")).toBeUndefined();
  });
});

describe("rpcLaunchSeed", () => {
  it("pins the welcome's Codex picks on a fresh row", () => {
    setLastAcpSetting("codex", "model", "gpt-6-luna");
    setLastAcpSetting("codex", "effort", "high");
    setLastAcpSetting("codex", "mode", "supervised");
    expect(rpcLaunchSeed({}, "codex", true, "claude-opus-5-5")).toEqual({
      row_only: true, model: "gpt-6-luna", effort: "high", permission_mode: "supervised",
    });
  });

  it("falls back to the caller's model when nothing was picked", () => {
    expect(rpcLaunchSeed({}, "codex", true, "gpt-6-sol")).toEqual({ row_only: true, model: "gpt-6-sol" });
  });

  it("leaves a resumed chat and a filled row alone", () => {
    setLastAcpSetting("codex", "model", "gpt-6-luna");
    expect(rpcLaunchSeed({ claudeSessionId: "thr_1" }, "codex", true)).toBeNull();
    expect(rpcLaunchSeed({ model: "gpt-6-sol" }, "codex", true)).toBeNull();
  });

  it("never seeds a generic ACP mode (a stale one would refuse the start)", () => {
    setLastAcpSetting("gemini", "mode", "yolo");
    expect(rpcLaunchSeed({}, "gemini", false)).toBeNull();
  });
});
