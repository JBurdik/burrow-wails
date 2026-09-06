/**
 * The compat shim must not run outside the Wails webview.
 *
 * vite.config.ts aliases `@tauri-apps/api/core` to core.ts for the mobile
 * bundle too, and the PWA pulls it in through `@/lib/config`, whose
 * module-level `configReady` invokes `read_config`. Once core.ts became a
 * pass-through to the /v2/ws transport, that call stopped throwing and started
 * retrying forever instead — so `configReady` never resolved and the phone
 * never restored its saved baseUrl/token. This freezes the fail-fast.
 *
 * Vitest runs without a DOM, so `window` is undefined here — exactly the
 * "no Wails runtime" condition (the PWA has a window but no `window.go`).
 */
import { describe, it, expect } from "vitest";
import { invoke } from "./core";

describe("invoke() outside the Wails webview", () => {
  it("rejects instead of hanging on the desktop transport", async () => {
    await expect(invoke("read_config")).rejects.toThrow(/no Wails runtime/);
  });

  it("rejects the client-side cases too, so no path slips past the guard", async () => {
    // detach_pty is answered by core.ts itself; it must still not pretend to
    // work in a context that has no desktop backend behind it.
    await expect(invoke("detach_pty")).rejects.toThrow(/no Wails runtime/);
  });
});
