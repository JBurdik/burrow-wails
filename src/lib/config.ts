import { invoke } from "@tauri-apps/api/core";

let cache: Record<string, unknown> = {};
let loaded = false;

export const configReady: Promise<void> = (async () => {
  try {
    const raw = await invoke<string>("read_config");
    cache = JSON.parse(raw) || {};
  } catch {
    cache = {};
  }
  loaded = true;
})();

function persist() {
  invoke("write_config", { content: JSON.stringify(cache) }).catch(() => {
    // best-effort; next setConfig call will retry with the latest cache
  });
}

export function getConfig<T>(key: string, fallback: T): T {
  if (!loaded) return fallback;
  return key in cache ? (cache[key] as T) : fallback;
}

export function setConfig(key: string, value: unknown): void {
  cache[key] = value;
  persist();
}

// One-time migration: pull a legacy localStorage value into config.json, then
// delete the localStorage key so this never runs twice. No-op if the config
// key is already populated (post-migration) or there was nothing to migrate.
export function migrateFromLocalStorage(lsKey: string, configKey: string): void {
  if (configKey in cache) return;
  const raw = localStorage.getItem(lsKey);
  if (raw === null) return;
  try {
    cache[configKey] = JSON.parse(raw);
  } catch {
    cache[configKey] = raw;
  }
  localStorage.removeItem(lsKey);
  persist();
}
