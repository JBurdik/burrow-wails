// Shim for "@tauri-apps/api/event"'s listen(), backed by Wails' generated
// runtime event bus (EventsOn/EventsOff). Tauri's payload shape is
// {event, payload}; Wails just hands the raw payload to the callback, so
// we wrap it back into the shape call-sites already expect.
import { EventsOn, EventsOff } from "../../../src-wails/frontend/wailsjs/runtime/runtime";

export type UnlistenFn = () => void;

export async function listen<T = unknown>(
  event: string,
  handler: (event: { event: string; payload: T }) => void,
): Promise<UnlistenFn> {
  EventsOn(event, (payload: T) => handler({ event, payload }));
  return () => EventsOff(event);
}

export async function emit(event: string, _payload?: unknown): Promise<void> {
  // Wails has no client->client emit; app-originated events only. Kept as
  // a no-op so call-sites that emit UI-local signals (e.g. float-focus-tab)
  // don't throw — cross-window signaling needs a proper Go-side relay,
  // not yet ported (see plan phase 7: float windows).
  console.warn(`[wails-compat] emit("${event}") is a no-op — not ported yet`);
}
