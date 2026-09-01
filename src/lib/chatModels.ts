import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { configReady, getConfig, setConfig } from "./config";

// Static per-agent model catalog. Only the native Claude CLI takes a --model
// flag we know the ids for up front; every other runtime negotiates its model
// after the session starts (ACP configOptions / Codex app-server), so those
// providers offer a single "Default" entry meaning "leave the agent alone".
export interface ModelEntry {
  id: string; // "" = provider default (no --model)
  label: string;
  // Codex reports the reasoning efforts each model accepts; the composer offers
  // exactly those. Absent for runtimes that don't publish a catalog.
  efforts?: string[];
  defaultEffort?: string;
}

export const MODELS_BY_AGENT: Record<string, ModelEntry[]> = {
  // Claude Code has no discovery call — t3code hardcodes the same way (its
  // ClaudeProvider.BUILT_IN_MODELS), just with extra `claude --version` semver
  // gates per model. ponytail: no gating here; an unsupported model now surfaces
  // the CLI's own error in the chat feed instead of failing silently.
  claude: [
    { id: "claude-opus-5", label: "Claude Opus 5" },
    { id: "claude-sonnet-5", label: "Claude Sonnet 5" },
    { id: "claude-fable-5", label: "Claude Fable 5" },
    { id: "claude-opus-4-8", label: "Claude Opus 4.8" },
    { id: "claude-sonnet-4-6", label: "Claude Sonnet 4.6" },
    { id: "claude-haiku-4-5-20251001", label: "Claude Haiku 4.5" },
  ],
};

const DEFAULT_ONLY: ModelEntry[] = [{ id: "", label: "Default" }];

// Models an agent reported at runtime (ACP configOptions / Codex `model/list`),
// cached so the welcome-screen picker can offer them before any session exists.
const SEEN_KEY = "chatModelsSeen";
const seen = ref<Record<string, ModelEntry[]>>({});
configReady.then(() => { seen.value = getConfig<Record<string, ModelEntry[]>>(SEEN_KEY, {}); });

export function learnModels(agentId: string, entries: ModelEntry[]): void {
  const usable = entries.filter((e) => e.id && e.label);
  if (!usable.length || MODELS_BY_AGENT[agentId]) return; // hardcoded catalogs win
  const prev = seen.value[agentId] ?? [];
  // Compare the efforts too — a cache written before Codex started reporting
  // them has the same ids, and skipping here would keep the composer's effort
  // pill hidden forever.
  const same = (a: ModelEntry, b: ModelEntry) =>
    a.id === b.id && (a.efforts ?? []).join() === (b.efforts ?? []).join();
  if (prev.length === usable.length && prev.every((p, i) => same(p, usable[i]))) return;
  seen.value = { ...seen.value, [agentId]: usable };
  setConfig(SEEN_KEY, seen.value);
}

// Ask the installed CLI for its catalog. Codex answers `model/list` over its
// app-server, so the picker can list real models before any chat exists — the
// result is cached, so this only costs a spawn the first time (and whenever the
// installed CLI's catalog changes).
const probed = new Set<string>();
export async function ensureModels(agentId: string, kind: string, cwd: string): Promise<void> {
  if (kind !== "codex" || MODELS_BY_AGENT[agentId] || probed.has(agentId)) return;
  probed.add(agentId);
  try {
    const models = await invoke<ModelEntry[]>("codex_list_models", { cwd });
    learnModels(agentId, models.map((m) => ({ id: m.id, label: m.label, efforts: m.efforts, defaultEffort: m.defaultEffort })));
  } catch {
    probed.delete(agentId); // transient (codex not installed yet / spawn race) — retry later
  }
}

export function modelsFor(agentId: string): ModelEntry[] {
  const catalog = MODELS_BY_AGENT[agentId];
  if (catalog) return catalog;
  const learned = seen.value[agentId];
  return learned?.length ? learned : DEFAULT_ONLY;
}

/** Reasoning efforts the given model accepts, empty when it publishes none. */
export function effortsFor(agentId: string, modelId: string): string[] {
  return modelsFor(agentId).find((m) => m.id === modelId)?.efforts ?? [];
}

export function defaultEffortFor(agentId: string, modelId: string): string | undefined {
  return modelsFor(agentId).find((m) => m.id === modelId)?.defaultEffort;
}

export function modelLabel(agentId: string, modelId: string): string {
  return modelsFor(agentId).find((m) => m.id === modelId)?.label ?? modelId ?? "Default";
}

// --- Favourites -------------------------------------------------------------
// Flat ordered list of "<agentId>/<modelId>" keys; order drives the ⌘1-9 hints.
const FAV_KEY = "chatFavoriteModels";

export function favKey(agentId: string, modelId: string): string {
  return `${agentId}/${modelId}`;
}

export const favorites = ref<string[]>([]);
configReady.then(() => {
  favorites.value = getConfig<string[]>(FAV_KEY, [favKey("claude", "claude-opus-5"), favKey("claude", "claude-sonnet-5")]);
});

export function isFavorite(agentId: string, modelId: string): boolean {
  return favorites.value.includes(favKey(agentId, modelId));
}

export function toggleFavorite(agentId: string, modelId: string): void {
  const k = favKey(agentId, modelId);
  favorites.value = favorites.value.includes(k) ? favorites.value.filter((x) => x !== k) : [...favorites.value, k];
  setConfig(FAV_KEY, favorites.value);
}

export function parseFav(key: string): { agentId: string; modelId: string } {
  const i = key.indexOf("/");
  return i === -1 ? { agentId: key, modelId: "" } : { agentId: key.slice(0, i), modelId: key.slice(i + 1) };
}
