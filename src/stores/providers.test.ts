/**
 * A provider updated on disk must leave `outdated` — that computed is the only
 * thing both update surfaces read, so it is what makes a row disappear from the
 * Settings banner and the toast at once.
 *
 * The regression this guards: the toast installed every outdated provider, then
 * bailed on the first bad result BEFORE re-probing anything. A provider that
 * updated fine kept its cached old version and stayed listed as out of date in
 * both places, right after the user watched it succeed.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

/** Version each binary reports. Tests move these to simulate an install. */
const onDisk: Record<string, string> = {};
/** Providers whose `update_provider` call fails. */
const installFails = new Set<string>();

vi.mock("@tauri-apps/api/core", () => ({
  invoke: async (cmd: string, a: Record<string, unknown>) => {
    if (cmd === "read_config") return "{}";
    if (cmd === "probe_provider") {
      const binary = a.binary as string;
      return { installed: true, path: `/usr/local/bin/${binary}`, version: onDisk[binary] ?? "", error: "" };
    }
    if (cmd === "update_provider") {
      const binary = a.binary as string;
      if (installFails.has(binary)) return { ok: false, error: `${binary} install failed` };
      onDisk[binary] = "2.0.0"; // the install landed
      return { ok: true, error: "" };
    }
    return {};
  },
}));

import { useProvidersStore } from "./providers";

/** claude + codex both installed at 1.0.0, both with 2.0.0 published. */
async function twoOutdatedProviders() {
  const store = useProvidersStore();
  onDisk.claude = "1.0.0";
  onDisk.codex = "1.0.0";
  await store.probeAll();
  store.latest = {
    claude: { version: "2.0.0", checkedAt: Date.now() },
    codex: { version: "2.0.0", checkedAt: Date.now() },
  };
  return store;
}

const outdatedIds = (store: ReturnType<typeof useProvidersStore>) =>
  [...new Set(store.outdated.map((a) => a.providerId))].sort();

describe("updateProvider reconciles the store with what is on disk", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    for (const k of Object.keys(onDisk)) delete onDisk[k];
    installFails.clear();
  });

  it("drops the provider from `outdated` once installed", async () => {
    const store = await twoOutdatedProviders();
    expect(outdatedIds(store)).toEqual(["claude", "codex"]);

    await store.updateProvider("claude");

    expect(outdatedIds(store)).toEqual(["codex"]);
  });

  it("keeps a provider listed when its install fails", async () => {
    const store = await twoOutdatedProviders();
    installFails.add("claude");

    const r = await store.updateProvider("claude");

    expect(r.ok).toBe(false);
    expect(outdatedIds(store)).toEqual(["claude", "codex"]);
  });

  it("a failing provider does not strand one that succeeded", async () => {
    const store = await twoOutdatedProviders();
    installFails.add("codex");

    // What the toast's "Update everything" button does.
    const results = await Promise.all(["claude", "codex"].map((id) => store.updateProvider(id)));

    expect(results.map((r) => r.ok)).toEqual([true, false]);
    // claude updated and must be gone from BOTH surfaces; only codex remains.
    expect(outdatedIds(store)).toEqual(["codex"]);
  });
});
