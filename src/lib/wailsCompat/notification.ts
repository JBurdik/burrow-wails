import { isDesktop } from "./desktop";
import { CoreNotifies } from "./bindings/burrow/desktopservice";
import { invoke } from "./core";

// Shim for "@tauri-apps/plugin-notification". Wails has no built-in native
// notification API; falls back to the Web Notification API, which works
// fine inside the WebKit/WebView2 webview on both macOS and Windows.
export async function isPermissionGranted(): Promise<boolean> {
  return typeof Notification !== "undefined" && Notification.permission === "granted";
}

export async function requestPermission(): Promise<"granted" | "denied" | "default"> {
  if (typeof Notification === "undefined") return "denied";
  if (await coreOwnsNotifications()) return "denied"; // no webview prompt for a banner it won't post
  return Notification.requestPermission();
}

// Plan 009: with an external core (dev/beta) the core posts OS notifications
// itself once macOS authorized it, so a webview banner would be a duplicate;
// until then — or for good, if macOS refuses the core — the webview keeps
// posting them. Re-checked at most every 30 s. Toasts are unaffected.
let coreCheckedAt = 0;
let coreOwns = false;
async function coreOwnsNotifications(): Promise<boolean> {
  if (!isDesktop()) return false;
  if (Date.now() - coreCheckedAt < 30_000) return coreOwns;
  coreCheckedAt = Date.now();
  try {
    coreOwns = (await CoreNotifies()) && (await invoke<{ notifications: string }>("privacy_status")).notifications === "granted";
  } catch {
    coreOwns = false;
  }
  return coreOwns;
}

export async function sendNotification(opts: string | { title?: string; body?: string }): Promise<void> {
  if (typeof Notification === "undefined") return;
  if (await coreOwnsNotifications()) return;
  if (typeof opts === "string") {
    new Notification(opts);
    return;
  }
  new Notification(opts.title ?? "", { body: opts.body });
}
