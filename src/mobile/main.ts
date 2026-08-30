import { createApp } from 'vue';
import { createPinia } from 'pinia';
import App from './App.vue';

const app = createApp(App);
app.use(createPinia());
app.mount('#mobile-app');

// Registered only so Chrome/Android offers "Install app" — the worker
// caches nothing. See public/sw.js.
if ('serviceWorker' in navigator) {
  // Relative to the document, so it also works under Tailscale Serve's /burrow/ prefix.
  navigator.serviceWorker.register('./sw.js').catch(() => {});
}
