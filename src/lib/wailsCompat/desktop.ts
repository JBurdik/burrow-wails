// True inside the native Wails window (macOS WKWebView's message handler, or
// WebView2 on Windows) — false in a paired browser, the PWA and `pnpm dev`.
// The one desktop check: everything that used to probe Wails v2's
// window.go / window.runtime globals asks this instead.
export function isDesktop(): boolean {
  if (typeof window === "undefined") return false;
  // The mobile bundle is never the desktop — but the native iOS shell
  // (src-ios/, Wails) registers the same `external` message handler.
  if (import.meta.env.VITE_TARGET === "mobile") return false;
  const w = window as any;
  return !!(w.webkit?.messageHandlers?.external || w.chrome?.webview);
}
