import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";
import { configReady, getConfig, setConfig } from "../lib/config";
import { KEY_COMMANDS, type KeyCommand, type KeyScope } from "../lib/keymap";
import { matchesShortcut } from "../lib/shortcuts";

// User rebinds live in config.json under `keybindings` as a flat
// { commandId: "⌘⇧O" } map — hand-editable, and the Settings → Keybindings UI
// writes the same map. An empty string means "unbound" (distinct from absent,
// which means "use the default").
const CONFIG_KEY = "keybindings";

export const useKeybindingsStore = defineStore("keybindings", () => {
  const overrides = ref<Record<string, string>>({});

  configReady.then(() => {
    overrides.value = { ...getConfig<Record<string, string>>(CONFIG_KEY, {}) };
  });

  watch(overrides, (v) => setConfig(CONFIG_KEY, v), { deep: true });

  function shortcut(id: string): string {
    const o = overrides.value[id];
    if (o !== undefined) return o;
    return KEY_COMMANDS.find((c) => c.id === id)?.def ?? "";
  }

  function set(id: string, sc: string) {
    // Rebinding onto a combo someone else holds would make one of them dead —
    // release the other one instead of silently shadowing it.
    if (sc) {
      for (const c of KEY_COMMANDS) {
        if (c.id !== id && shortcut(c.id) === sc) overrides.value[c.id] = "";
      }
    }
    overrides.value[id] = sc;
  }

  function reset(id: string) {
    delete overrides.value[id];
  }

  function resetAll() {
    overrides.value = {};
  }

  function matches(e: KeyboardEvent, id: string): boolean {
    return matchesShortcut(e, shortcut(id));
  }

  /** Commands of one scope, with their live binding — for the two key listeners. */
  function inScope(scope: KeyScope): (KeyCommand & { keys: string })[] {
    return KEY_COMMANDS.filter((c) => c.scope === scope).map((c) => ({ ...c, keys: shortcut(c.id) }));
  }

  const groups = computed(() => {
    const out: { label: string; commands: (KeyCommand & { keys: string; custom: boolean })[] }[] = [];
    for (const c of KEY_COMMANDS) {
      const g = out.find((x) => x.label === c.group) ?? (out.push({ label: c.group, commands: [] }), out[out.length - 1]);
      g.commands.push({ ...c, keys: shortcut(c.id), custom: overrides.value[c.id] !== undefined });
    }
    return out;
  });

  return { overrides, shortcut, set, reset, resetAll, matches, inScope, groups };
});
