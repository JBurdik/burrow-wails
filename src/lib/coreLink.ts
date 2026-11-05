import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { appTransport } from "@/lib/wailsCompat/core";
import { isDesktop } from "@/lib/wailsCompat/desktop";
import { QuitApp, SetDockBadge, ShowMain } from "@/lib/wailsCompat/bindings/burrow/desktopservice";

/**
 * The main window's half of plan 009: when the backend is a separate core
 * process, the core owns the tray and tells this window what the
 * in-process tray used to do directly:
 *
 *   ui-open {route}   tray / notification click while this UI is up
 *   core-quit         tray Quit — quit this process too, or it would just
 *                     start a fresh core on the dropped socket
 *   waiting-count n   the Dock badge, which belongs to this process
 *
 * Desktop main window only: a phone gets the same bus and must not act on it.
 */
export async function startCoreLink(navigate: (route: string) => void): Promise<() => void> {
  if (!isDesktop()) return () => {};
  const badge = (n: unknown) => void SetDockBadge(typeof n === "number" ? n : 0).catch(() => {});
  const readCount = () => void invoke<number>("waiting_count").then(badge).catch(() => {});
  const offs: UnlistenFn[] = await Promise.all([
    listen<{ route?: string }>("ui-open", (e) => {
      void ShowMain().catch(() => {});
      if (e.payload?.route) navigate(e.payload.route);
    }),
    listen("core-quit", () => void QuitApp().catch(() => {})),
    listen<number>("waiting-count", (e) => badge(e.payload)),
  ]);
  readCount();
  const offResync = appTransport().onResync(readCount);
  return () => {
    offs.forEach((off) => off());
    offResync();
  };
}
