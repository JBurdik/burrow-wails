import { defineStore } from "pinia";
import { ref, watch } from "vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "../lib/config";

// A Claude "profile" = a launch identity: which binary to run, which config dir
// (CLAUDE_CONFIG_DIR — sessions/auth/settings live there, so a profile is really
// a separate Claude account/config), and any extra flags. Mission Control picks
// one per task; the choice persists on the task so `--resume` reuses the same
// config dir (a session created under one config dir is invisible to another).
export interface ClaudeProfile {
  id: string;
  name: string;
  command: string;    // binary to launch, default "claude"
  configDir: string;  // CLAUDE_CONFIG_DIR (empty = the user's default)
  args: string;       // extra flags appended before the prompt
  orgAccount: boolean; // org/team accounts can't use OAuth usage API — skip to local JSONL scan
}

const CONFIG_KEY = "claudeProfiles";
const LEGACY_STORAGE_KEY = "agentic-ide.claude-profiles";

// The built-in profile is always present and can't be deleted — it's the plain
// `claude` launch with no overrides (matches the pre-profiles behaviour).
export const DEFAULT_PROFILE_ID = "default";

function defaults(): ClaudeProfile[] {
  return [{ id: DEFAULT_PROFILE_ID, name: "Default", command: "claude", configDir: "", args: "", orgAccount: false }];
}

function normalize(parsed: unknown): ClaudeProfile[] {
  if (Array.isArray(parsed) && parsed.length) {
    const list: ClaudeProfile[] = parsed.map((p: any) => ({
      id: String(p.id),
      name: String(p.name ?? "Profile"),
      command: String(p.command ?? "claude"),
      configDir: String(p.configDir ?? ""),
      args: String(p.args ?? ""),
      orgAccount: Boolean(p.orgAccount ?? false),
    }));
    // Guarantee the default profile exists (first slot).
    if (!list.some((p) => p.id === DEFAULT_PROFILE_ID)) list.unshift(defaults()[0]);
    return list;
  }
  return defaults();
}

let counter = 0;
function makeId(): string {
  counter++;
  return `profile-${counter}-${counter * 7 + 13}`;
}

export const useProfilesStore = defineStore("claude-profiles", () => {
  const profiles = ref<ClaudeProfile[]>(defaults());

  configReady.then(() => {
    migrateFromLocalStorage(LEGACY_STORAGE_KEY, CONFIG_KEY);
    profiles.value = normalize(getConfig<unknown>(CONFIG_KEY, defaults()));
  });

  watch(profiles, (v) => setConfig(CONFIG_KEY, v), { deep: true });

  function get(id: string | null | undefined): ClaudeProfile | undefined {
    if (!id) return profiles.value.find((p) => p.id === DEFAULT_PROFILE_ID);
    return profiles.value.find((p) => p.id === id) ?? profiles.value.find((p) => p.id === DEFAULT_PROFILE_ID);
  }

  function add() {
    profiles.value.push({ id: makeId(), name: "New profile", command: "claude", configDir: "", args: "", orgAccount: false });
  }

  function update(id: string, patch: Partial<Omit<ClaudeProfile, "id">>) {
    const p = profiles.value.find((x) => x.id === id);
    if (p) Object.assign(p, patch);
  }

  function remove(id: string) {
    if (id === DEFAULT_PROFILE_ID) return; // the default profile is permanent
    profiles.value = profiles.value.filter((x) => x.id !== id);
  }

  return { profiles, get, add, update, remove };
});
