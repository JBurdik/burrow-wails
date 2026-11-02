/**
 * Per-project overrides for settings that are otherwise global. Anything absent
 * falls back to the app-wide value, so a project only carries what it actually
 * differs on.
 */
import { getConfig, setConfig } from "@/lib/config";

export interface ProjectSettings {
  /** Chat agent the composer starts on for this project. */
  agentId?: string;
  /** Model the composer starts on for this project. */
  modelId?: string;
  /** Where `git worktree add` puts this repo's worktrees. */
  worktreesDir?: string;
}

const KEY = "projectSettings";

type Store = Record<string, ProjectSettings>;

export function getProjectSettings(wsId: number): ProjectSettings {
  return getConfig<Store>(KEY, {})[String(wsId)] ?? {};
}

export function setProjectSettings(wsId: number, patch: ProjectSettings) {
  const all = { ...getConfig<Store>(KEY, {}) };
  const merged = { ...all[String(wsId)], ...patch };
  // An empty string means "use the global value" — don't keep it around.
  for (const k of Object.keys(merged) as (keyof ProjectSettings)[]) {
    if (!merged[k]) delete merged[k];
  }
  if (Object.keys(merged).length) all[String(wsId)] = merged;
  else delete all[String(wsId)];
  setConfig(KEY, all);
}

/** Where a new worktree of `workspace` on `branch` goes: the project's worktrees dir, else `defaultDir`. */
export function worktreePath(workspace: { id: number; parent_id?: number | null; path: string }, branch: string, defaultDir: string): string {
  const repo = workspace.path.split("/").filter(Boolean).pop() || "repo";
  const root = getProjectSettings(workspace.parent_id ?? workspace.id).worktreesDir || defaultDir;
  return `${root}/${repo}/${branch.replaceAll("/", "-")}`;
}
