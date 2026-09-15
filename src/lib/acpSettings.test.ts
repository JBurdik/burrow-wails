import { beforeEach, describe, expect, it, vi } from "vitest";

const store: Record<string, unknown> = {};
vi.mock("@/lib/config", () => ({
  getConfig: <T,>(key: string, fallback: T) => (key in store ? (store[key] as T) : fallback),
  setConfig: (key: string, value: unknown) => { store[key] = value; },
}));

const { getAcpChatSetting, getLastAcpSetting, setAcpChatSetting, setLastAcpSetting } = await import("./acpSettings");

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
});
