import { createApp } from "vue";
import { isDesktop } from "./lib/wailsCompat/desktop";
import { createPinia } from "pinia";
import { hideSplash } from "./lib/splash";

const pinia = createPinia();

// Every external http(s) link (chat markdown, settings, window.open, middle
// click, ...) goes to the system browser. Wails' WKWebView has no navigation or
// new-window delegate, so left alone it navigates the app window itself to the
// page — no back, no close, the app is gone until restart.
const isExternal = (url: string) => {
  try {
    const u = new URL(url, location.href);
    return /^https?:$/.test(u.protocol) && u.origin !== location.origin && isDesktop();
  } catch { return false; }
};
const openExternal = (url: string) => void import("./lib/wailsCompat/shell").then((m) => m.open(url));
const onLinkClick = (e: MouseEvent) => {
  const a = (e.target as Element | null)?.closest?.("a[href]") as HTMLAnchorElement | null;
  if (!a || !isExternal(a.href)) return;
  e.preventDefault();
  if (e.type === "click" || e.button === 1) openExternal(a.href);
};
document.addEventListener("click", onLinkClick, true);
document.addEventListener("auxclick", onLinkClick, true);
const nativeOpen = window.open.bind(window);
window.open = (url?: string | URL, ...rest) => {
  if (url && isExternal(String(url))) { openExternal(new URL(String(url), location.href).href); return null; }
  return nativeOpen(url, ...rest);
};

async function boot() {
  if (isDesktop()) void import("./lib/focusReport").then((m) => m.startFocusReport(location.hash.startsWith("#/window/") ? location.hash.slice(2) : "main"));
  // A secondary native window (src/lib/windows.ts): its own small root, never
  // App.vue — that one runs app-wide side effects exactly one window may own.
  if (location.hash.startsWith("#/window/")) {
    document.getElementById("app")!.style.height = "100vh";
    const { default: WindowApp } = await import("./WindowApp.vue");
    createApp(WindowApp).use(pinia).mount("#app");
    return;
  }

  const { default: App } = await import("./App.vue");
  const { router } = await import("./router");
  const app = createApp(App);
  app.use(pinia);
  // Only the main window is routed; secondary windows address themselves by
  // their "/window/..." hash and have no view state to route.
  app.use(router);
  app.mount("#app");
}

boot();
// ponytail: safety net so a failed ws.load() never leaves the splash up for good.
setTimeout(hideSplash, 8000);
