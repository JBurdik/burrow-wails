/**
 * The compat shim must not run with no endpoint behind it.
 *
 * vite.config.ts aliases `@tauri-apps/api/core` to core.ts for the mobile
 * bundle too, and the PWA pulls it in through `@/lib/config`, whose
 * module-level `configReady` invokes `read_config`. Once core.ts became a
 * pass-through to the /v2/ws transport, that call stopped throwing and started
 * retrying forever instead — so `configReady` never resolved and the phone
 * never restored its saved state. This freezes the fail-fast.
 *
 * Since phase 6 there are two ways to HAVE an endpoint (a Wails runtime, or
 * stored pairing credentials), so the guard is about neither being present —
 * which is exactly an unpaired phone before its pairing screen has been
 * through.
 *
 * Vitest runs without a DOM, so `window` is undefined here: no Wails runtime,
 * and localStorage is stubbed empty below so there are no credentials either.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { invoke } from "./core";

const store = new Map<string, string>();
(globalThis as any).localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
};

beforeEach(() => store.clear());

describe("invoke() with no endpoint", () => {
  it("rejects instead of hanging on a transport that cannot connect", async () => {
    await expect(invoke("read_config")).rejects.toThrow(/no endpoint/);
  });

  it("rejects the client-side cases too, so no path slips past the guard", async () => {
    // detach_pty is answered by core.ts itself; it must still not pretend to
    // work in a context that has no backend behind it.
    await expect(invoke("detach_pty")).rejects.toThrow(/no endpoint/);
  });

  it("names the desktop-only commands as such rather than throwing a TypeError", async () => {
    // On a paired device these have no Wails binding to call. Reaching one is
    // a bug, and it should read as one instead of as `undefined is not a
    // function` from inside a generated shim.
    store.set(
      "burrow.remote.credentials",
      JSON.stringify({ baseUrl: "https://host", deviceToken: "t", environmentId: "e" }),
    );
    await expect(invoke("open_git_panel_window")).rejects.toThrow(/desktop-only/);
  });
});
