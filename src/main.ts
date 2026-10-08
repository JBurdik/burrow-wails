import { createApp } from "vue";
import { createPinia } from "pinia";

const pinia = createPinia();

// Every external http(s) link (chat markdown, settings, window.open, middle
// click, ...) goes to the system browser. Wails' WKWebView has no navigation or
// new-window delegate, so left alone it navigates the app window itself to the
// page — no back, no close, the app is gone until restart.
const isExternal = (url: string) => {
  try {
    const u = new URL(url, location.href);
    return /^https?:$/.test(u.protocol) && u.origin !== location.origin && !!(window as any).runtime;
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
  // In Tauri, window label is synchronously accessible via internals
  const label: string = (window as any).__TAURI_INTERNALS__?.metadata?.currentWindow?.label ?? "";
  const isGitPanel = label === "gitpanel";

  if (isGitPanel) {
    document.getElementById("app")!.style.height = "100vh";
    const { default: GitPanel } = await import("./components/GitPanel.vue");
    const app = createApp(GitPanel);
    app.use(pinia);
    app.mount("#app");
  } else {
    const { default: App } = await import("./App.vue");
    const { router } = await import("./router");
    const app = createApp(App);
    app.use(pinia);
    // Only the main window is routed. The detached git panel is a different
    // window with its own single-purpose root and no view state to address.
    app.use(router);
    app.mount("#app");
  }
}

boot();
