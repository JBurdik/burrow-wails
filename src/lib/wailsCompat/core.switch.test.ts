import { describe, it, expect, vi } from "vitest";

const made: { close: ReturnType<typeof vi.fn> }[] = [];
vi.mock("@/runtime/transport", () => ({
  createTransport: () => {
    const t = { close: vi.fn() };
    made.push(t);
    return t;
  },
}));

const store = new Map<string, string>();
(globalThis as any).localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
};

const { remoteTransport, switchEnvironment } = await import("./core");
const { activeEnvironmentId, saveRemoteCredentials } = await import("@/runtime/remoteEndpoint");

describe("switchEnvironment", () => {
  it("closes the old socket, activates the new machine and reloads", () => {
    saveRemoteCredentials({ baseUrl: "https://a", deviceToken: "a", environmentId: "A" });
    saveRemoteCredentials({ baseUrl: "https://b", deviceToken: "b", environmentId: "B" });
    const first = remoteTransport();
    const reload = vi.fn();
    switchEnvironment("A", reload);
    expect(first.close).toHaveBeenCalledOnce();
    expect(activeEnvironmentId()).toBe("A");
    expect(reload).toHaveBeenCalledOnce();
    expect(remoteTransport()).not.toBe(first);
  });
});
