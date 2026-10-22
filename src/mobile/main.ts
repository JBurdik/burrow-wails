import { createApp } from "vue";
import { clearRemoteCredentials, loadRemoteCredentials } from "@/runtime/remoteEndpoint";

// A phone has no devtools. Without this a boot failure is a blank screen the
// user can only report as "blank"; with it they read (and can send) the cause.
let shownError = false;
function showBootError(e: unknown) {
  const host = document.getElementById("app");
  if (shownError || !host || host.childElementCount > 0) return; // the app did paint: leave it alone
  shownError = true;
  const msg = e instanceof Error ? `${e.name}: ${e.message}\n${e.stack ?? ""}` : String(e);
  host.innerHTML = "";
  const box = document.createElement("div");
  box.style.cssText = "padding:24px;font:13px/1.5 -apple-system,system-ui,sans-serif;color:#ECE6E9;background:#241A20;min-height:100dvh";
  const pre = document.createElement("pre");
  pre.style.cssText = "white-space:pre-wrap;word-break:break-word;font:11px/1.4 ui-monospace,monospace;color:#D9827A";
  pre.textContent = msg;
  const row = document.createElement("div");
  row.style.cssText = "display:flex;gap:8px;margin-top:16px";
  for (const [label, run] of [["Reload", () => location.reload()], ["Forget this device", () => { clearRemoteCredentials(); location.reload(); }]] as const) {
    const b = document.createElement("button");
    b.textContent = label;
    b.style.cssText = "padding:10px 14px;border-radius:8px;border:1px solid #3D3137;background:#2C2127;color:#ECE6E9;font:inherit";
    b.onclick = run;
    row.append(b);
  }
  const h = document.createElement("div");
  h.textContent = "Burrow could not start";
  h.style.cssText = "font-size:16px;font-weight:600;margin-bottom:8px";
  box.append(h, pre, row);
  host.append(box);
}
window.addEventListener("error", (ev) => showBootError(ev.error ?? ev.message));
window.addEventListener("unhandledrejection", (ev) => showBootError(ev.reason));

// The remote client IS the desktop app: same App.vue, same stores, same
// transport — only the ticket source differs (src/lib/wailsCompat/core.ts
// picks the paired credentials when there is no Wails runtime). A phone gets
// the same UI in its compact layout (App.vue). What is left here is pairing.
if (loadRemoteCredentials()) {
  void import("@/main").then(async () => {
    // A revoke is a 401 on the next ticket, which clears the credentials
    // (remoteEndpoint.ts). Reload into the pairing screen instead of a UI
    // that can no longer reach anything.
    const { appTransport } = await import("@/lib/wailsCompat/core");
    appTransport().onState((up) => {
      if (!up && !loadRemoteCredentials()) location.reload();
    });
  }).catch(showBootError);
} else {
  // App.vue carries the global stylesheet (tokens, Tailwind theme); importing
  // the module applies it without mounting the app.
  void Promise.all([import("./ConnectView.vue"), import("@/App.vue")]).then(([m]) =>
    createApp(m.default).mount("#app"),
  ).catch(showBootError);
}

// Registered only so Chrome/Android offers "Install app" — the worker
// caches nothing. See public/sw.js.
if ("serviceWorker" in navigator) {
  // Relative to the document, so it also works under Tailscale Serve's /burrow/ prefix.
  navigator.serviceWorker.register("./sw.js").catch(() => {});
}
