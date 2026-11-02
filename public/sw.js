// ponytail: no caching on purpose — the whole app is a live WS session, so
// an offline shell would only show a "disconnected" screen. This exists
// because Chrome/Android gates the install prompt on a registered service
// worker with a fetch handler, and for Web Push (src-wails/push.go). Add a
// cache here if offline is ever actually wanted.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));
self.addEventListener("fetch", () => {});

// Payload from src-wails/pushtrigger.go: {title, body, url, tag}. tag is
// chat:<id> / pty:<id>, so a newer status replaces the older one.
self.addEventListener("push", (e) => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch {}
  e.waitUntil(self.registration.showNotification(d.title || "Burrow", {
    body: d.body || "",
    tag: d.tag,
    renotify: !!d.tag,
    icon: "./icons/icon-192.png",
    data: { url: d.url || "" },
  }));
});

// url is a hash route ("#/ws/3/tab/7"), resolved against the scope so it
// also works under Tailscale Serve's /burrow/ prefix.
self.addEventListener("notificationclick", (e) => {
  e.notification.close();
  const url = new URL(e.notification.data?.url || "", self.registration.scope).href;
  e.waitUntil((async () => {
    const open = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const client = open[0];
    if (!client) return self.clients.openWindow(url);
    await client.focus();
    // Same document, new fragment: a hash navigation, not a reload.
    return client.navigate(url).catch(() => client.postMessage({ type: "burrow-open", url }));
  })());
});
