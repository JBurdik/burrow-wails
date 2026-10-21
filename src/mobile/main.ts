import { createApp } from "vue";
import { loadRemoteCredentials } from "@/runtime/remoteEndpoint";

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
  });
} else {
  // App.vue carries the global stylesheet (tokens, Tailwind theme); importing
  // the module applies it without mounting the app.
  void Promise.all([import("./ConnectView.vue"), import("@/App.vue")]).then(([m]) =>
    createApp(m.default).mount("#app"),
  );
}

// Registered only so Chrome/Android offers "Install app" — the worker
// caches nothing. See public/sw.js.
if ("serviceWorker" in navigator) {
  // Relative to the document, so it also works under Tailscale Serve's /burrow/ prefix.
  navigator.serviceWorker.register("./sw.js").catch(() => {});
}
