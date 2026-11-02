// Secondary native windows (Settings, popped-out tabs). Each loads this same
// frontend at a "/window/..." hash route; main.ts boots that into WindowApp.vue
// instead of App.vue, so a second window never runs the main window's
// app-wide side effects (control bridge acks, chat attention, notifications).
import { OpenWindow, CloseWindow } from "@/lib/wailsCompat/bindings/burrow/desktopservice";
import { isDesktop } from "@/lib/wailsCompat/desktop";
import { compact } from "@/lib/viewport";

export const WINDOW_ROUTE_PREFIX = "#/window/";

export function isSecondaryWindow(): boolean {
  return typeof location !== "undefined" && location.hash.startsWith(WINDOW_ROUTE_PREFIX);
}

/** Native windows exist only in the desktop app at full width — a paired
 *  browser and the phone shell keep their in-page surfaces. */
export function canOpenWindows(): boolean {
  return isDesktop() && !compact.value;
}

/** `floating`: no fullscreen button, so a tiling WM (AeroSpace) floats it
 *  like a dialog instead of tiling it — Settings, and popped-out tabs when
 *  `ui.floatPopouts` is on. */
export function openWindow(key: string, route: string, title: string, width = 960, height = 680, floating = false): Promise<void> {
  return OpenWindow(key, route, title, width, height, floating);
}

export function closeWindow(key: string): Promise<void> {
  return CloseWindow(key);
}
