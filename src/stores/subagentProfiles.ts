import { shallowRef, readonly } from "vue";
import { defineStore } from "pinia";
import { configReady, getConfig, setConfig } from "@/lib/config";
import { defaultSubagentProfiles, normalizeSubagentProfiles, type SubagentProfile } from "@/lib/subagentProfiles";

export const useSubagentProfilesStore = defineStore("subagentProfiles", () => {
  const profiles = shallowRef(defaultSubagentProfiles());
  const ready = shallowRef(false);
  const loaded = configReady.then(() => {
    profiles.value = normalizeSubagentProfiles(getConfig("subagentProfiles", null));
    ready.value = true;
  });
  function save(profile: SubagentProfile) {
    if (!ready.value) return;
    profiles.value = normalizeSubagentProfiles(profiles.value.map((row) => row.id === profile.id ? { ...profile } : row));
    setConfig("subagentProfiles", profiles.value);
  }
  function reset(id: string) {
    const profile = defaultSubagentProfiles().find((row) => row.id === id);
    if (profile) save(profile);
  }
  function find(id: string) { return profiles.value.find((row) => row.id === id); }
  return { whenReady: () => loaded, profiles: readonly(profiles), ready: readonly(ready), save, reset, find };
});
