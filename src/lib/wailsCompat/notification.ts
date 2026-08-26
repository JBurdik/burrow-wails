// Shim for "@tauri-apps/plugin-notification". Wails has no built-in native
// notification API; falls back to the Web Notification API, which works
// fine inside the WebKit/WebView2 webview on both macOS and Windows.
export async function isPermissionGranted(): Promise<boolean> {
  return typeof Notification !== "undefined" && Notification.permission === "granted";
}

export async function requestPermission(): Promise<"granted" | "denied" | "default"> {
  if (typeof Notification === "undefined") return "denied";
  return Notification.requestPermission();
}

export async function sendNotification(opts: string | { title?: string; body?: string }): Promise<void> {
  if (typeof Notification === "undefined") return;
  if (typeof opts === "string") {
    new Notification(opts);
    return;
  }
  new Notification(opts.title ?? "", { body: opts.body });
}
