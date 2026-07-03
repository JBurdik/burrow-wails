import { ref, computed } from "vue";
import { defineStore } from "pinia";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

// Lazy-imported so a browser-only `pnpm dev` (no Tauri) doesn't crash on the
// plugin's window.__TAURI__ access at module load.
type UpdateHandle = {
  version: string;
  currentVersion: string;
  body?: string;
  downloadAndInstall: (cb?: (e: DownloadEvent) => void) => Promise<void>;
};
type DownloadEvent =
  | { event: "Started"; data: { contentLength?: number } }
  | { event: "Progress"; data: { chunkLength: number } }
  | { event: "Finished" };

const DISMISS_LEGACY_KEY = "agentic-ide.update.dismissed"; // version the user dismissed
const DISMISS_CONFIG_KEY = "updateDismissedVersion";

export const useUpdateStore = defineStore("update", () => {
  const available = ref(false);
  const newVersion = ref("");
  const currentVersion = ref("");
  const notes = ref("");
  const checking = ref(false);
  const downloading = ref(false);
  const installed = ref(false); // download+install done, awaiting relaunch
  const error = ref<string | null>(null);
  const progress = ref(0); // 0..1, -1 if content length unknown
  const lastChecked = ref<number | null>(null);

  let handle: UpdateHandle | null = null;

  // Banner shows only if an update is available AND the user hasn't dismissed
  // this exact version (dismissal is per-version so a newer one re-nags).
  const dismissed = ref("");
  configReady.then(() => {
    migrateFromLocalStorage(DISMISS_LEGACY_KEY, DISMISS_CONFIG_KEY);
    dismissed.value = getConfig<string>(DISMISS_CONFIG_KEY, "");
  });
  const bannerVisible = computed(
    () => available.value && !installed.value && newVersion.value !== dismissed.value,
  );

  function dismiss() {
    dismissed.value = newVersion.value;
    setConfig(DISMISS_CONFIG_KEY, newVersion.value);
  }

  async function check(opts: { silent?: boolean } = {}) {
    if (checking.value || downloading.value) return;
    error.value = null;
    checking.value = true;
    try {
      const { check: tauriCheck } = await import("@tauri-apps/plugin-updater");
      const update = (await tauriCheck()) as UpdateHandle | null;
      lastChecked.value = Date.now();
      if (update) {
        handle = update;
        available.value = true;
        newVersion.value = update.version;
        currentVersion.value = update.currentVersion;
        notes.value = update.body ?? "";
      } else {
        available.value = false;
        handle = null;
      }
    } catch (e) {
      // In browser-only dev the plugin import/IPC fails — swallow unless asked.
      error.value = String(e);
      if (!opts.silent) console.warn("[update] check failed:", e);
    } finally {
      checking.value = false;
    }
  }

  async function downloadAndInstall() {
    if (!handle || downloading.value) return;
    error.value = null;
    downloading.value = true;
    progress.value = 0;
    let total = 0;
    let got = 0;
    try {
      await handle.downloadAndInstall((e) => {
        if (e.event === "Started") {
          total = e.data.contentLength ?? 0;
          progress.value = total > 0 ? 0 : -1;
        } else if (e.event === "Progress") {
          got += e.data.chunkLength;
          if (total > 0) progress.value = Math.min(1, got / total);
        } else if (e.event === "Finished") {
          progress.value = 1;
        }
      });
      installed.value = true;
    } catch (e) {
      error.value = String(e);
      console.warn("[update] install failed:", e);
    } finally {
      downloading.value = false;
    }
  }

  async function relaunch() {
    const { relaunch: doRelaunch } = await import("@tauri-apps/plugin-process");
    await doRelaunch();
  }

  return {
    available,
    newVersion,
    currentVersion,
    notes,
    checking,
    downloading,
    installed,
    error,
    progress,
    lastChecked,
    bannerVisible,
    check,
    downloadAndInstall,
    relaunch,
    dismiss,
  };
});
