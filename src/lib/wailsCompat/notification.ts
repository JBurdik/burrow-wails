import { isDesktop } from "./desktop";
import { CoreNotifies } from "./bindings/burrow/desktopservice";

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

// Plan 009: with an external core (dev/beta) the core posts every OS
// notification itself, so a webview banner would be a duplicate. Toasts stay.
let coreNotifies: Promise<boolean> | null = null;
function coreOwnsNotifications(): Promise<boolean> {
  if (!isDesktop()) return Promise.resolve(false);
  coreNotifies ??= CoreNotifies().catch(() => false);
  return coreNotifies;
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
