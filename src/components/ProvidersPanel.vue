<template>
  <section class="flex flex-col gap-3.5">
    <div v-if="addOpen" class="fixed inset-0 z-20" @click="addOpen = false" />

    <!-- Header -->
    <div class="flex items-center gap-2.5">
      <div class="flex items-baseline gap-2.5">
        <h2 class="text-[15px] font-semibold text-foreground">Providers</h2>
        <span class="text-xs text-muted-foreground">Agent CLIs Burrow can launch — in a terminal tab or as a chat</span>
      </div>
      <div class="ml-auto flex items-center gap-2">
        <span class="text-[11px] text-muted-foreground/70">{{ checkedLabel }}</span>
        <button
          class="flex rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground disabled:opacity-40"
          :disabled="store.probing"
          title="Re-check installed versions"
          @click="store.probeAll()"
        >
          <PhArrowsClockwise :size="13" :class="store.probing && 'animate-spin'" />
        </button>
        <div class="relative">
          <Button variant="outline" size="sm" class="px-2" title="Add a provider instance" @click.stop="addOpen = !addOpen">
            <PhPlus :size="11" />
          </Button>
          <div
            v-if="addOpen"
            class="absolute right-0 top-[calc(100%+6px)] z-30 grid w-[240px] gap-0.5 rounded-lg border border-border bg-base p-2 shadow-[0_12px_32px_rgba(0,0,0,0.5)]"
            @click.stop
          >
            <div class="px-2 pb-1.5 pt-1 text-[10px] font-semibold uppercase tracking-[0.04em] text-muted-foreground">New instance</div>
            <button
              v-for="p in store.catalog"
              :key="p.id"
              class="flex w-full items-center gap-2.5 rounded px-2.5 py-1.5 text-left text-xs text-secondary-foreground hover:bg-hover hover:text-foreground"
              @click="onAdd(p.id)"
            >
              <component :is="agentIconComp(p.icon)" :size="13" :style="{ color: p.color }" />
              <span class="flex-1 font-medium">{{ p.label }}</span>
              <code v-if="p.binary" class="rounded bg-panel px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">{{ p.binary }}</code>
            </button>
          </div>
        </div>
      </div>
    </div>
    <div class="h-px bg-border" />

    <div class="flex min-h-[420px] gap-3.5">
      <!-- Instance rail -->
      <aside class="flex w-[260px] shrink-0 flex-col gap-px overflow-hidden rounded-lg border border-border bg-panel/40">
        <button
          v-for="a in store.instances"
          :key="a.id"
          class="flex items-center gap-2.5 border-b border-border/60 px-3 py-2.5 text-left last:border-b-0 hover:bg-hover"
          :class="[selectedId === a.id && 'bg-hover', !a.enabled && 'opacity-55']"
          @click="selectedId = a.id"
        >
          <component :is="agentIconComp(a.icon)" :size="16" class="shrink-0" :style="{ color: a.color }" />
          <span class="flex min-w-0 flex-1 flex-col gap-0.5">
            <span class="flex items-baseline gap-1.5">
              <span class="truncate text-[13px] text-foreground">{{ a.name }}</span>
              <code class="shrink-0 font-mono text-[10px] text-muted-foreground/70">{{ versionBadge(a) }}</code>
            </span>
            <span class="truncate text-[11px]" :class="statusClass(a)">{{ statusLine(a) }}</span>
          </span>
          <Switch
            class="shrink-0"
            :checked="a.enabled"
            :title="a.enabled ? 'Disable' : 'Enable'"
            @click.stop
            @update:checked="(v: boolean) => store.update(a.id, { enabled: v })"
          />
        </button>
      </aside>

      <!-- Detail -->
      <div v-if="inst" class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border">
        <header class="flex items-center gap-2 border-b border-border px-4 py-3">
          <component :is="agentIconComp(inst.icon)" :size="16" :style="{ color: inst.color }" />
          <span class="text-[14px] font-semibold text-foreground">{{ inst.name }}</span>
          <code class="rounded bg-panel px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">{{ inst.id }}</code>
          <button
            v-if="inst.builtin"
            class="ml-auto rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground"
            title="Reset to defaults"
            @click="store.reset(inst.id)"
          >
            <PhArrowCounterClockwise :size="13" />
          </button>
          <button
            v-else
            class="ml-auto rounded p-1 text-muted-foreground hover:bg-destructive/12 hover:text-destructive"
            title="Delete instance"
            @click="onDelete(inst.id)"
          >
            <PhTrash :size="13" />
          </button>
        </header>

        <!-- Tabs -->
        <div class="flex gap-4 border-b border-border px-4">
          <button
            v-for="t in tabs"
            :key="t"
            class="border-b-2 border-transparent py-2 text-[12px] text-muted-foreground hover:text-foreground"
            :class="tab === t && 'border-accent text-foreground'"
            @click="tab = t"
          >{{ t }}</button>
        </div>

        <div class="flex-1 overflow-y-auto px-4 py-4">
          <!-- Configuration -->
          <div v-if="tab === 'Configuration'" class="flex flex-col gap-4">
            <SettingField label="Display name" hint="Optional label shown in the provider list.">
              <input v-model="inst.name" type="text" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground focus:border-accent/60 focus:outline-none" />
            </SettingField>

            <SettingField label="Accent color" hint="Used to distinguish this instance in picker rails and model lists.">
              <div class="flex items-center gap-1.5">
                <button
                  v-for="c in SWATCHES"
                  :key="c"
                  class="h-[22px] w-[22px] rounded-full border-2"
                  :style="{ background: c, borderColor: inst.color.toLowerCase() === c ? 'var(--foreground)' : 'transparent' }"
                  :title="c"
                  @click="inst.color = c"
                />
                <label class="relative ml-1 flex h-[22px] w-[28px] cursor-pointer items-center justify-center rounded border border-border" title="Custom color">
                  <span class="h-[14px] w-[14px] rounded-full" :style="{ background: inst.color }" />
                  <input v-model="inst.color" type="color" class="absolute inset-0 h-full w-full cursor-pointer opacity-0" />
                </label>
              </div>
            </SettingField>

            <SettingField label="Icon">
              <div class="flex flex-wrap items-center gap-1.5">
                <button
                  v-for="key in AGENT_ICON_KEYS"
                  :key="key"
                  class="inline-flex h-[28px] w-[28px] items-center justify-center rounded-lg border border-border bg-base hover:bg-hover"
                  :class="inst.icon === key && 'border-accent/60 bg-hover'"
                  :style="{ color: inst.color }"
                  :title="key"
                  @click="inst.icon = key"
                >
                  <component :is="agentIconComp(key)" :size="15" />
                </button>
              </div>
            </SettingField>

            <SettingField label="Environment variables" hint="Passed to this instance only — API keys, base URLs, other per-instance CLI settings.">
              <template #action>
                <Button variant="outline" size="sm" @click="addEnv"><PhPlus :size="10" /> Add</Button>
              </template>
              <div v-if="!envRows.length" class="text-[11px] text-muted-foreground/60">None.</div>
              <div v-for="(row, i) in envRows" :key="i" class="grid grid-cols-[1fr_1.4fr_auto] gap-1.5">
                <input v-model="row.k" type="text" placeholder="KEY" class="rounded-md border border-border bg-base px-2 py-1 font-mono text-[11px] text-foreground" @input="commitEnv" />
                <input v-model="row.v" type="text" placeholder="value" class="rounded-md border border-border bg-base px-2 py-1 font-mono text-[11px] text-foreground" @input="commitEnv" />
                <button class="rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" @click="removeEnv(i)"><PhX :size="11" /></button>
              </div>
            </SettingField>

            <SettingField label="Binary path" :hint="`Program run by this instance. Empty uses ${catalogBinary || 'the command below'}.`">
              <input
                v-model="inst.binary"
                type="text"
                :placeholder="catalogBinary || 'command'"
                class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground focus:border-accent/60 focus:outline-none"
              />
            </SettingField>

            <SettingField
              v-if="catalog.supportsConfigDir"
              label="CLAUDE_CONFIG_DIR path"
              hint="Sessions, auth and settings live here — a separate dir is a separate account. Empty uses your default."
            >
              <div class="flex gap-1.5">
                <input
                  v-model="inst.configDir"
                  type="text"
                  placeholder="(default)"
                  class="min-w-0 flex-1 rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground focus:border-accent/60 focus:outline-none"
                />
                <Button variant="outline" size="sm" @click="browseConfigDir"><PhFolderOpen :size="11" /></Button>
              </div>
            </SettingField>
          </div>

          <!-- Models -->
          <div v-else-if="tab === 'Models'" class="flex flex-col gap-1">
            <div v-if="!models.length" class="text-[11px] text-muted-foreground/60">
              No models reported yet — start a chat with this provider once.
            </div>
            <button
              v-for="m in models"
              :key="m.id"
              class="flex items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-hover"
              @click="onToggleFavorite(m.id)"
            >
              <PhStar :size="13" :weight="isFav(m.id) ? 'fill' : 'regular'" :class="isFav(m.id) ? 'text-accent' : 'text-muted-foreground/50'" />
              <span class="flex-1 truncate text-xs text-secondary-foreground">{{ m.label }}</span>
              <code class="font-mono text-[10px] text-muted-foreground/60">{{ m.id }}</code>
            </button>
          </div>
        </div>

        <!-- Advanced -->
        <details class="border-t border-border px-4 py-2.5">
          <summary class="cursor-pointer list-none text-[12px] text-muted-foreground hover:text-foreground">
            <PhCaretRight :size="11" class="mr-1 inline align-[-1px]" /> Advanced
          </summary>
          <div class="flex flex-col gap-4 pt-3.5">
            <SettingField label="Terminal args" hint="Extra flags used only when launching this provider in a terminal tab.">
              <input v-model="inst.terminalArgs" type="text" placeholder="--dangerously-skip-permissions" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground focus:border-accent/60 focus:outline-none" />
            </SettingField>

            <div class="grid grid-cols-2 gap-3">
              <SettingField label="Terminal shortcut">
                <ShortcutRecorder v-model="inst.terminalShortcut" />
              </SettingField>
              <SettingField label="New chat shortcut">
                <ShortcutRecorder v-model="inst.chatShortcut" :disabled="inst.transport === 'none'" />
              </SettingField>
            </div>

            <SettingField label="Chat transport" hint="How the embedded chat talks to this provider. “None” makes it terminal-only.">
              <select v-model="inst.transport" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground focus:border-accent/60 focus:outline-none">
                <option value="none">None — terminal launches only</option>
                <option value="claude-cli">Native Claude CLI (stream-json)</option>
                <option value="codex-app-server">Native Codex app-server (JSON-RPC)</option>
                <option value="acp">ACP (any ACP CLI)</option>
              </select>
            </SettingField>

            <template v-if="inst.transport === 'acp'">
              <SettingField label="Transport args" hint="Adapter args appended to the binary, e.g. --acp or @scope/pkg.">
                <input :value="inst.transportArgs.join(' ')" type="text" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground focus:border-accent/60 focus:outline-none" @input="setTransportArgs" />
              </SettingField>
              <SettingField label="Env injection kind" hint="Which provider-specific env the backend injects (auth, executable overrides).">
                <select v-model="inst.kind" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground focus:border-accent/60 focus:outline-none">
                  <option value="custom">custom (no special env)</option>
                  <option value="claude">claude (CLAUDE_CODE_EXECUTABLE, OAuth)</option>
                  <option value="gemini">gemini</option>
                  <option value="codex">codex (forward API keys)</option>
                </select>
              </SettingField>
            </template>

            <label v-if="catalog.supportsConfigDir" class="flex items-center gap-2 text-[12px] text-secondary-foreground">
              <input v-model="inst.orgAccount" type="checkbox" class="accent-[var(--accent)]" />
              Org / team account — skip the OAuth usage API and read local session files instead
            </label>

            <div class="flex flex-col gap-1 rounded-lg border border-border bg-white/[0.025] p-2.5">
              <span class="text-[11px] font-semibold text-foreground/75">Resolved launch</span>
              <code class="w-fit max-w-full truncate font-mono text-[10px] text-muted-foreground">{{ store.commandLine(inst) }}</code>
              <code v-if="probe?.path" class="w-fit max-w-full truncate font-mono text-[10px] text-muted-foreground/60">{{ probe.path }}</code>
            </div>
          </div>
        </details>
      </div>

      <div v-else class="flex flex-1 items-center justify-center text-[13px] text-muted-foreground/40">
        No providers configured.
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { PhPlus, PhX, PhTrash, PhArrowsClockwise, PhArrowCounterClockwise, PhCaretRight, PhStar, PhFolderOpen } from "@phosphor-icons/vue";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import SettingField from "@/components/SettingField.vue";
import ShortcutRecorder from "@/components/ShortcutRecorder.vue";
import { useProvidersStore, type ProviderInstance } from "@/stores/providers";
import { providerFor } from "@/lib/providers";
import { agentIconComp, AGENT_ICON_KEYS } from "@/lib/agentIcons";
import { modelsFor, isFavorite, toggleFavorite, ensureModels } from "@/lib/chatModels";
import { pickDir } from "@/lib/pickPath";
import { flushConfig } from "@/lib/config";
import { invoke } from "@tauri-apps/api/core";

const props = defineProps<{ focusId?: string }>();

// A per-instance CLAUDE_CONFIG_DIR is also a dir Burrow installs its status
// hooks into, and the backend reads that list from config.json — so on close,
// wait for the store's write to land, then re-install. Doing it per keystroke
// would race the debounced write and reinstall dozens of times.
onBeforeUnmount(async () => {
  await flushConfig();
  await invoke("reinstall_status_hooks").catch(() => {});
});

const store = useProvidersStore();

const SWATCHES = ["#d97757", "#3b82f6", "#22c55e", "#f97316", "#ef4444", "#a855f7", "#06b6d4"];

const selectedId = ref(props.focusId ?? store.instances[0]?.id ?? "");
const tab = ref<"Configuration" | "Models">("Configuration");
const addOpen = ref(false);

const inst = computed(() => store.instances.find((a) => a.id === selectedId.value));
const catalog = computed(() => providerFor(inst.value?.providerId ?? "custom"));
const catalogBinary = computed(() => catalog.value.binary);
const tabs = computed<("Configuration" | "Models")[]>(() =>
  inst.value && inst.value.transport !== "none" ? ["Configuration", "Models"] : ["Configuration"],
);

// A provider whose chat contract was just removed must not be stuck on Models.
watch(tabs, (t) => { if (!t.includes(tab.value)) tab.value = "Configuration"; });
watch(() => props.focusId, (id) => { if (id) selectedId.value = id; });
// Keep a selection when the current instance is deleted.
watch(() => store.instances.length, () => {
  if (!store.instances.some((a) => a.id === selectedId.value)) selectedId.value = store.instances[0]?.id ?? "";
});

onMounted(() => { void store.probeAll(); });

// --- Status line ------------------------------------------------------------
const probe = computed(() => (inst.value ? store.status[inst.value.id] : undefined));

function versionBadge(a: ProviderInstance): string {
  const s = store.status[a.id];
  return s?.version ? `v${s.version}` : "";
}

function statusLine(a: ProviderInstance): string {
  const s = store.status[a.id];
  if (!a.enabled) return "Disabled";
  if (!s) return "Checking…";
  if (!s.installed) return s.error || "Not installed";
  return s.error ? `Installed · ${s.error}` : "Installed";
}

function statusClass(a: ProviderInstance): string {
  const s = store.status[a.id];
  if (!a.enabled) return "text-muted-foreground/60";
  if (s && !s.installed) return "text-destructive/80";
  return "text-muted-foreground";
}

const checkedLabel = computed(() => {
  if (store.probing) return "Checking…";
  const times = Object.values(store.status).map((s) => s.checkedAt).filter(Boolean);
  if (!times.length) return "";
  const age = Date.now() - Math.max(...times);
  if (age < 60_000) return "Checked just now";
  const mins = Math.round(age / 60_000);
  if (mins < 60) return `Checked ${mins}m ago`;
  return `Checked ${Math.round(mins / 60)}h ago`;
});

// --- Env editor -------------------------------------------------------------
const envRows = ref<{ k: string; v: string }[]>([]);
watch(inst, (a) => { envRows.value = a ? Object.entries(a.env).map(([k, v]) => ({ k, v })) : []; }, { immediate: true });

function commitEnv() {
  if (!inst.value) return;
  const env: Record<string, string> = {};
  for (const r of envRows.value) if (r.k.trim()) env[r.k.trim()] = r.v;
  inst.value.env = env;
}
function addEnv() { envRows.value.push({ k: "", v: "" }); }
function removeEnv(i: number) { envRows.value.splice(i, 1); commitEnv(); }

function setTransportArgs(e: Event) {
  if (!inst.value) return;
  inst.value.transportArgs = (e.target as HTMLInputElement).value.split(/\s+/).filter(Boolean);
}

async function browseConfigDir() {
  if (!inst.value) return;
  const picked = await pickDir({ title: "Choose config dir", start: inst.value.configDir || "~/" });
  if (picked) inst.value.configDir = picked;
}

// --- Models -----------------------------------------------------------------
const models = computed(() => (inst.value ? modelsFor(inst.value.id) : []));
watch([inst, tab], () => {
  if (inst.value && tab.value === "Models") void ensureModels(inst.value.id, inst.value.kind, "");
});
function isFav(modelId: string) { return inst.value ? isFavorite(inst.value.id, modelId) : false; }
function onToggleFavorite(modelId: string) { if (inst.value) toggleFavorite(inst.value.id, modelId); }

// --- Actions ----------------------------------------------------------------
function onAdd(providerId: string) {
  addOpen.value = false;
  selectedId.value = store.add(providerId).id;
  void store.probe(selectedId.value);
}

function onDelete(id: string) {
  store.remove(id);
}
</script>
