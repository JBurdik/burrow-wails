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
