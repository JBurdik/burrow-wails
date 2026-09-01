import { defineStore } from "pinia";
import { ref, computed, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { configReady, getConfig, setConfig } from "../lib/config";
import {
  PROVIDER_CATALOG,
  providerFor,
  newInstance,
  seedInstances,
  binaryFor,
  commandLine,
  type ProviderInstance,
} from "../lib/providers";
import { migrateLegacyConfigs, type LegacyConfigs } from "../lib/providersMigrate";

export type { ProviderInstance, ChatTransport } from "../lib/providers";
export { transportLabel, providerFor, binaryFor, commandLine, chatTransportFor } from "../lib/providers";

const CONFIG_KEY = "providers";
const ALIAS_KEY = "providerAliases";
const STATUS_KEY = "providerStatus";

/** Legacy config keys, read once by the migration and then left alone. */
const LEGACY_KEYS = {
  agents: "agentPresets",
  chatAgents: "chatAgentPresets",
  profiles: "claudeProfiles",
} as const;

/** Probe result for one instance, cached so a restart shows a version immediately. */
export interface ProviderStatus {
  installed: boolean;
  path: string;
  version: string;
  error: string;
  checkedAt: number;
}

/** Fill in fields added by later releases so an old config still parses. */
function normalize(parsed: unknown): ProviderInstance[] | null {
  if (!Array.isArray(parsed) || !parsed.length) return null;
  return parsed.map((raw) => {
    const a = raw as Partial<ProviderInstance>;
    return newInstance(String(a.providerId ?? "custom"), {
      ...a,
      id: String(a.id ?? "custom"),
      args: [...(a.args ?? [])],
      transportArgs: [...(a.transportArgs ?? [])],
      env: { ...(a.env ?? {}) },
    });
  });
}

let counter = 0;
function makeId(providerId: string): string {
  counter++;
  return `${providerId}-${counter}-${Date.now().toString(36)}`;
}

export const useProvidersStore = defineStore("providers", () => {
  const instances = ref<ProviderInstance[]>(seedInstances());
  /** legacy id (old agent / chat-agent / profile id) → current instance id. */
  const aliases = ref<Record<string, string>>({});
  const status = ref<Record<string, ProviderStatus>>({});
  const ready = ref(false);

  configReady.then(() => {
    const saved = normalize(getConfig<unknown>(CONFIG_KEY, null));
    if (saved) {
      instances.value = saved;
      aliases.value = getConfig<Record<string, string>>(ALIAS_KEY, {});
    } else {
      // First run on this release: fold the three legacy stores into one list.
      const legacy: LegacyConfigs = {
        agents: getConfig<unknown>(LEGACY_KEYS.agents, null),
        chatAgents: getConfig<unknown>(LEGACY_KEYS.chatAgents, null),
        profiles: getConfig<unknown>(LEGACY_KEYS.profiles, null),
      };
      const result = migrateLegacyConfigs(legacy);
      instances.value = result.instances;
      aliases.value = result.aliases;
      setConfig(CONFIG_KEY, result.instances);
      setConfig(ALIAS_KEY, result.aliases);
    }
    status.value = getConfig<Record<string, ProviderStatus>>(STATUS_KEY, {});
    ready.value = true;
  });

  watch(instances, (v) => ready.value && setConfig(CONFIG_KEY, v), { deep: true });
  watch(status, (v) => ready.value && setConfig(STATUS_KEY, v), { deep: true });

  /** Instances the rest of the app may use — disabled ones are invisible. */
  const active = computed(() => instances.value.filter((a) => a.enabled));
  /** Active instances offering an embedded chat. */
  const chatAgents = computed(() => active.value.filter((a) => a.transport !== "none"));

  /**
   * Resolve an id that may be a live instance id OR a legacy id persisted on an
   * old chat/task, via the migration's alias map.
   */
  function byId(id: string | null | undefined): ProviderInstance | undefined {
    if (!id) return chatAgents.value[0] ?? instances.value[0];
    const direct = instances.value.find((a) => a.id === id);
    if (direct) return direct;
    const aliased = aliases.value[id];
    return aliased ? instances.value.find((a) => a.id === aliased) : undefined;
  }

  /** Same as byId but never undefined — for call sites that must render something. */
  function resolve(id: string | null | undefined): ProviderInstance {
    return byId(id) ?? chatAgents.value[0] ?? instances.value[0];
  }

  function add(providerId: string): ProviderInstance {
    const p = providerFor(providerId);
    const dupes = instances.value.filter((a) => a.providerId === p.id).length;
    const inst = newInstance(p.id, {
      id: makeId(p.id),
      name: dupes ? `${p.label} ${dupes + 1}` : p.label,
    });
    instances.value.push(inst);
    return inst;
  }

  function update(id: string, patch: Partial<Omit<ProviderInstance, "id">>) {
    const a = instances.value.find((x) => x.id === id);
    if (a) Object.assign(a, patch);
  }

  function remove(id: string) {
    instances.value = instances.value.filter((x) => x.id !== id);
  }

  /** Restore a builtin instance to its catalog defaults, keeping its id. */
  function reset(id: string) {
    const i = instances.value.findIndex((a) => a.id === id);
    if (i === -1 || !instances.value[i].builtin) return;
    instances.value[i] = newInstance(instances.value[i].providerId, { id, builtin: true });
  }

  /** Move the instance at `from` to `to`; array order drives the toolbar. */
  function move(from: number, to: number) {
    const list = instances.value;
    if (from < 0 || from >= list.length || to < 0 || to >= list.length || from === to) return;
    const [item] = list.splice(from, 1);
    list.splice(to, 0, item);
  }

  function setStatus(id: string, s: Omit<ProviderStatus, "checkedAt">) {
    status.value = { ...status.value, [id]: { ...s, checkedAt: Date.now() } };
  }

  const probing = ref(false);

  /** Ask the backend whether one instance's binary exists, and its version. */
  async function probe(id: string, cwd = "") {
    const inst = instances.value.find((a) => a.id === id);
    if (!inst) return;
    try {
      const r = await invoke<Omit<ProviderStatus, "checkedAt">>("probe_provider", { binary: binaryFor(inst), cwd });
      setStatus(id, r);
    } catch (e) {
      setStatus(id, { installed: false, path: "", version: "", error: String(e) });
    }
  }

  /** Probe every instance in parallel — cheap, and the page shows them at once. */
  async function probeAll(cwd = "") {
    if (probing.value) return;
    probing.value = true;
    try {
      await Promise.all(instances.value.map((a) => probe(a.id, cwd)));
    } finally {
      probing.value = false;
    }
  }

  return {
    instances, aliases, status, ready, probing, active, chatAgents,
    byId, resolve, add, update, remove, reset, move, setStatus, probe, probeAll,
    binaryFor, commandLine,
    catalog: PROVIDER_CATALOG,
  };
});
