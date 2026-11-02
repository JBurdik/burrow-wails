import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { appTransport } from "@/lib/wailsCompat/core";

let cache: Record<string, unknown> = {};
let loaded = false;
// JSON of each key as last written or received. setConfig skips a write that
// would not change it — otherwise two windows applying each other's
// config-changed (new array/object refs fire the store watchers that save)
// would echo the same value back and forth forever. A snapshot, not the cache
// value itself, because callers mutate cached objects in place.
const lastJSON: Record<string, string> = {};

type ConfigListener = (key: string) => void;
const listeners = new Set<ConfigListener>();

async function readAll(): Promise<void> {
  try {
    const raw = await invoke<string>("read_config");
    cache = JSON.parse(raw) || {};
  } catch {
    cache = {};
  }
  loaded = true;
}

export const configReady: Promise<void> = readAll();

// Every window (and paired phone) holds its own copy of config.json. Writes go
// out one key at a time (config_set) and come back to everyone as
// config-changed, so another window's Settings change lands here instead of
// being overwritten by this copy's next save. config-changed is not in the
// resume ring, so a resync re-reads the whole file.
void configReady.then(() => {
  try {
    void listen<{ key: string; value: unknown }>("config-changed", ({ payload }) => {
      cache[payload.key] = payload.value;
      lastJSON[payload.key] = JSON.stringify(payload.value ?? null);
      for (const fn of listeners) fn(payload.key);
    }).catch(() => {});
    appTransport().onResync(() => {
      void readAll().then(() => {
        for (const fn of listeners) fn("*");
      });
    });
  } catch {
    // No endpoint (an unpaired phone, unit tests): nothing to stay in sync with.
  }
});

/** Called with the changed key ("*" after a full re-read) when config.json
 *  changes underneath this client — another window or device saved it. */
export function onConfigChanged(fn: ConfigListener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Run `apply` once config is loaded and again whenever one of `keys` changes
 *  in another window/device (or after a resync). For module refs that hold a
 *  copy of a key and write it back whole: without the re-apply, this window's
 *  stale copy would overwrite the other window's change on its next save. */
export function followConfig(keys: string[], apply: () => void): void {
  void configReady.then(() => {
    apply();
    onConfigChanged((key) => { if (key === "*" || keys.includes(key)) apply(); });
  });
}

let pending: Promise<unknown> = Promise.resolve();

function persistKey(key: string) {
  const json = JSON.stringify(cache[key] ?? null);
  if (lastJSON[key] === json) return;
  lastJSON[key] = json;
  pending = invoke("config_set", { key, value: cache[key] ?? null }).catch(() => {
    // best-effort; forget the snapshot so the next setConfig of this key retries
    if (lastJSON[key] === json) delete lastJSON[key];
  });
}

/** Resolves once the last setConfig has hit disk — for callers whose next step
 *  is backend code that reads config.json. */
export function flushConfig(): Promise<void> {
  return pending.then(() => undefined);
}

export function getConfig<T>(key: string, fallback: T): T {
  if (!loaded) return fallback;
  return key in cache ? (cache[key] as T) : fallback;
}

export function setConfig(key: string, value: unknown): void {
  cache[key] = value;
  persistKey(key);
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
  persistKey(configKey);
}
