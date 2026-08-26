import { createApp } from "vue";
import { createPinia } from "pinia";

const pinia = createPinia();

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
    const app = createApp(App);
    app.use(pinia);
    app.mount("#app");
  }
}

boot();
