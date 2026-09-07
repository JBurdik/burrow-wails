// Shim for "@tauri-apps/api/event"'s listen(), backed by the /v2/ws
// transport. Event names on the wire are identical to the bus names in Go, so
// there is no translation table here to forget an entry in.
import { EventsOn } from "../../../src-wails/frontend/wailsjs/runtime/runtime";
import { appTransport } from "./core";

export type UnlistenFn = () => void;

/**
 * The events that never reach the bus, so the socket cannot carry them.
 *
 * `busEmit` is the single door for every event a client may care about, and
 * the WS sink is fed from there — but a handful of emitters deliberately call
 * `runtime.EventsEmit` instead, because only the native window can consume
 * them (src-wails/events_test.go's `wailsRuntimeAllowlist` is the list, with
 * a reason per file). Those keep arriving on the Wails runtime:
 *
 *   menu-*          main.go, native menu items
 *   lsp-msg-*       lsp.go
 *   float-*         stubs.go's snapshot protocol (core.ts keeps the matching
 *                   send_float_snapshot/notify_float_grid calls on the Wails
 *                   bindings for the same reason)
 *   extension-task: extension_bridge.go
 *   update:*        updater.go's progress (updater.ts subscribes with its own
 *                   EventsOn today; listed so routing it through listen()
 *                   later cannot silently go quiet)
 *
 * If one of these ever becomes something a remote client needs, the fix is to
 * move its emitter to busEmit — not to add a second delivery path here.
 */
const DESKTOP_ONLY_EVENT_PREFIXES = ["menu-", "lsp-msg-", "float-", "extension-task:", "update:"];

function isDesktopOnly(event: string): boolean {
  return DESKTOP_ONLY_EVENT_PREFIXES.some((prefix) => event.startsWith(prefix));
}

export async function listen<T = unknown>(
  event: string,
  handler: (event: { event: string; payload: T }) => void,
): Promise<UnlistenFn> {
  // Tauri's payload shape is {event, payload}; both the wire and the Wails
  // runtime hand us the raw payload, so wrap it back into the shape
  // call-sites already expect.
  const deliver = (payload: T) => handler({ event, payload });
  // EventsOn returns a cancel for THIS callback; the old shim used
  // EventsOff(event), which tore down every listener on the name.
  // A paired device has no Wails runtime, so a desktop-only name can only be
  // silence there. Returning a no-op unlisten rather than throwing: these are
  // subscriptions a shared component sets up unconditionally (the updater
  // banner, the LSP bridge), and a throw on the phone would take the whole
  // component down over an event that was never going to fire.
  if (isDesktopOnly(event)) {
    return typeof EventsOn === "function" && typeof window !== "undefined" && (window as any).runtime
      ? EventsOn(event, deliver)
      : () => {};
  }
  return appTransport().listen<T>(event, deliver);
}

export async function emit(event: string, _payload?: unknown): Promise<void> {
  // There is no client->client emit: app-originated events only. Kept as a
  // no-op so call-sites that emit UI-local signals don't throw.
  console.warn(`[wails-compat] emit("${event}") is a no-op — not ported`);
}
