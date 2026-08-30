// ponytail: no caching on purpose — the whole app is a live WS session, so
// an offline shell would only show a "disconnected" screen. This exists
// solely because Chrome/Android gates the install prompt on a registered
// service worker with a fetch handler. Add a cache here if offline is
// ever actually wanted.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));
self.addEventListener("fetch", () => {});
