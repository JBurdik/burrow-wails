// "Open in" targets + the last one used per project — shared by
// ThreadDetailsCard's Open-in menu, ⌘O and chat file links. Targets are the
// installed apps Go finds (ListOpenTargets in fs.go).
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { configReady, getConfig, setConfig, migrateFromLocalStorage, onConfigChanged } from "@/lib/config";

export type OpenTarget = { id: string; name: string; icon: string };
export const FINDER_TARGET: OpenTarget = { id: "finder", name: "Finder", icon: "" };

export const openTargets = ref<OpenTarget[]>([FINDER_TARGET]);
let loading: Promise<void> | null = null;
export function loadOpenTargets(): Promise<void> {
  loading ??= invoke<OpenTarget[]>("list_open_targets", {})
    .then((t) => { openTargets.value = t; })
    .catch((e) => console.error("list_open_targets failed", e));
  return loading;
}

// Keyed by project folder path, not global.
const LAST_OPEN_IN_LEGACY_KEY = "tb-last-open-in";
const LAST_OPEN_IN_CONFIG_KEY = "titlebarLastOpenInByProject";
export const lastOpenInByProject = ref<Record<string, string>>({});
const load = () => {
  const stored = getConfig<Record<string, string> | string>(LAST_OPEN_IN_CONFIG_KEY, {});
  lastOpenInByProject.value = typeof stored === "string" ? {} : stored;
};
configReady.then(() => {
  migrateFromLocalStorage(LAST_OPEN_IN_LEGACY_KEY, LAST_OPEN_IN_CONFIG_KEY);
  load();
  onConfigChanged((key) => { if (key === LAST_OPEN_IN_CONFIG_KEY || key === "*") load(); });
});

export function lastOpenIn(project: string): string {
  return lastOpenInByProject.value[project] || "finder";
}

/** The target a file opens in: the project's last choice, unless that only
 *  reveals (Finder) — then the first installed editor. */
export function fileOpenTarget(project: string, targets: OpenTarget[] = openTargets.value): string {
  const last = lastOpenIn(project);
  if (last !== "finder") return last;
  return targets.find((t) => t.id !== "finder")?.id ?? "finder";
}

export function targetName(id: string): string {
  return openTargets.value.find((t) => t.id === id)?.name ?? (id === "finder" ? "Finder" : id.split("/").pop()?.replace(/\.app$/, "") ?? id);
}

/** Open `path` in `target`; `remember` makes it the project's last choice. */
export async function openPathIn(path: string, target: string, opts: { project?: string; line?: number } = {}) {
  if (opts.project) {
    lastOpenInByProject.value = { ...lastOpenInByProject.value, [opts.project]: target };
    setConfig(LAST_OPEN_IN_CONFIG_KEY, lastOpenInByProject.value);
  }
  await invoke("open_path_in", { path, target, line: opts.line ?? 0 }).catch((e) => console.error("open_path_in failed", e));
}
