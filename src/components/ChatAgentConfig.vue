<template>
  <div class="fixed inset-0 z-[2000] flex items-center justify-center bg-black/50 backdrop-blur-[2px]" @mousedown.self="$emit('close')">
    <div class="max-h-[86vh] w-[720px] max-w-[92vw] overflow-auto rounded-[14px] border border-border bg-panel text-foreground/85 shadow-[0_20px_60px_rgba(0,0,0,0.6)]">
      <header class="flex items-center justify-between border-b border-border px-4.5 py-3.5">
        <h2 class="m-0 text-[15px] font-semibold">Chat agents</h2>
        <button class="rounded-md p-1 text-muted-foreground hover:bg-hover hover:text-foreground" @click="$emit('close')"><PhX :size="16" /></button>
      </header>

      <div class="flex">
        <!-- Agent list -->
        <aside class="flex w-[200px] shrink-0 flex-col gap-0.5 border-r border-border p-2.5">
          <button
            v-for="a in chatAgents.agents"
            :key="a.id"
            class="flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-left text-xs text-foreground/75 hover:bg-hover"
            :class="{ 'bg-accent/18 text-foreground': a.id === selectedId }"
            @click="selectedId = a.id"
          >
            <component :is="agentIconComp(a.icon)" :size="13" :style="{ color: a.color }" />
            <span class="flex-1 truncate">{{ a.name }}</span>
            <span class="font-mono text-[8px] text-muted-foreground/70">{{ transportLabel(a.transport) }}</span>
          </button>
          <button class="mt-1.5 flex items-center justify-center gap-1 rounded-lg border border-dashed border-border bg-white/4 px-2 py-1.5 text-[11px] text-muted-foreground hover:bg-white/8" @click="onAdd"><PhPlus :size="12" /> New agent</button>
        </aside>

        <!-- Editor -->
        <section v-if="agent" class="flex min-w-0 flex-1 flex-col gap-3 px-4.5 py-4">
          <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
            <span>Name</span>
            <input v-model="agent.name" type="text" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none" />
          </label>

          <div class="flex flex-col gap-1 text-[11px] text-muted-foreground">
            <span>Icon &amp; color</span>
            <div class="flex flex-wrap items-center gap-1.5">
              <button
                v-for="key in AGENT_ICON_KEYS"
                :key="key"
                class="inline-flex h-[30px] w-[30px] items-center justify-center rounded-lg border border-border bg-base hover:bg-white/6"
                :class="{ 'border-current bg-white/6': agent.icon === key }"
                :style="{ color: agent.color }"
                :title="key"
                @click="agent.icon = key"
              >
                <component :is="agentIconComp(key)" :size="16" />
              </button>
              <input v-model="agent.color" type="color" class="ml-1 h-[30px] w-[34px] cursor-pointer rounded-lg border border-border bg-base p-0.5" title="Accent color" />
            </div>
          </div>

          <div class="flex flex-col gap-1 text-[11px] text-muted-foreground">
            <span>Launch shortcut</span>
            <div class="flex items-center gap-1.5">
              <button
                class="inline-flex min-w-[60px] items-center justify-center rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-[11px] text-muted-foreground hover:border-border hover:text-secondary-foreground"
                :class="{ 'border-accent/40 bg-accent/8 text-pink-400': recordingId === agent.id }"
                :title="recordingId === agent.id ? 'Press keys… (Esc to cancel)' : 'Click to set shortcut — always opens a new chat'"
                @click="startRecording(agent.id, $event)"
                @keydown="onRecordKey(agent.id, $event)"
                @blur="recordingId === agent.id && (recordingId = null)"
              >
                {{ recordingId === agent.id ? "Press…" : (agent.shortcut || "—") }}
              </button>
              <button v-if="agent.shortcut && recordingId !== agent.id" class="flex shrink-0 items-center justify-center rounded p-0.5 text-muted-foreground/60 hover:bg-destructive/12 hover:text-destructive" title="Clear shortcut" @click.stop="agent.shortcut = ''">
                <PhX :size="11" />
              </button>
            </div>
          </div>

          <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
            <span>Transport</span>
            <select v-model="agent.transport" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none">
              <option value="claude-cli">Native Claude CLI (stream-json)</option>
              <option value="codex-app-server">Native Codex app-server (JSON-RPC)</option>
              <option value="acp">ACP (any ACP CLI)</option>
            </select>
          </label>

          <template v-if="agent.transport === 'acp'">
            <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
              <span>Command</span>
              <input v-model="agent.command" type="text" placeholder="npx | gemini | codex | opencode" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none" />
            </label>
            <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
              <span>Args</span>
              <input :value="agent.args.join(' ')" type="text" placeholder="@scope/pkg --flag" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none" @input="setArgs(($event.target as HTMLInputElement).value)" />
            </label>
            <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
              <span>Kind</span>
              <select v-model="agent.kind" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none">
                <option value="custom">custom (no special env)</option>
                <option value="claude">claude (CLAUDE_CODE_EXECUTABLE, OAuth)</option>
                <option value="gemini">gemini</option>
                <option value="codex">codex (forward API keys)</option>
              </select>
            </label>

            <div class="flex flex-col gap-1.5">
              <div class="flex items-center justify-between text-[11px] text-muted-foreground">
                <span>Environment variables</span>
                <button class="inline-flex items-center gap-0.5 rounded p-1 text-muted-foreground hover:bg-white/8 hover:text-foreground" @click="addEnv"><PhPlus :size="11" /> add</button>
              </div>
              <div v-for="(row, i) in envRows" :key="i" class="grid grid-cols-[1fr_1.4fr_auto] gap-1.5">
                <input v-model="row.k" type="text" placeholder="KEY" class="rounded-md border border-border bg-base px-2 py-1 font-mono text-[11px] text-foreground" @input="commitEnv" />
                <input v-model="row.v" type="text" placeholder="value" class="rounded-md border border-border bg-base px-2 py-1 font-mono text-[11px] text-foreground" @input="commitEnv" />
                <button class="inline-flex items-center gap-0.5 rounded p-1 text-muted-foreground hover:bg-white/8 hover:text-foreground" @click="removeEnv(i)"><PhX :size="11" /></button>
              </div>
            </div>
          </template>
          <div v-else class="flex flex-col gap-1.5 rounded-lg border border-border bg-white/[0.025] p-2.5">
            <span class="text-[11px] font-semibold text-foreground/75">{{ agent.transport === 'codex-app-server' ? 'Codex app-server' : 'Claude Code CLI' }}</span>
            <code class="w-fit max-w-full truncate font-mono text-[10px] text-muted-foreground">{{ agent.transport === 'codex-app-server' ? 'codex app-server' : 'claude --input-format stream-json --output-format stream-json' }}</code>
            <p class="m-0 text-[11px] leading-relaxed text-muted-foreground/70">{{ agent.transport === 'codex-app-server' ? 'Uses your local codex login and the native JSON-RPC app-server. No ACP adapter is involved.' : 'Uses your local claude login and Claude’s native newline-delimited stream-json protocol. No ACP adapter is involved.' }}</p>
          </div>

          <div class="mt-auto pt-2">
            <button v-if="agent.builtin" class="rounded-lg border border-border bg-white/6 px-3 py-1.5 text-xs text-foreground/80 hover:bg-white/10" @click="onReset">Reset to default</button>
            <button v-else class="rounded-lg border border-destructive/30 bg-white/6 px-3 py-1.5 text-xs text-red-400 hover:bg-destructive/12" @click="onDelete">Delete</button>
          </div>
        </section>
      </div>

      <!-- Per-project overrides (only when opened from a project chat) -->
      <section v-if="cwd" class="flex flex-col gap-2.5 border-t border-border px-4.5 py-3.5">
        <h3 class="m-0 flex items-center gap-2 text-xs font-semibold text-foreground/70">This project <code class="font-mono text-[10px] text-muted-foreground">{{ cwdShort }}</code></h3>
        <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
          <span>CLAUDE_CONFIG_DIR</span>
          <input :value="proj.claude_config_dir ?? ''" type="text" placeholder="(default)" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none" @change="setProj('claude_config_dir', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="flex flex-col gap-1 text-[11px] text-muted-foreground">
          <span>.env file</span>
          <input :value="proj.env_file ?? ''" type="text" placeholder=".env" class="rounded-lg border border-border bg-base px-2.5 py-1.5 font-sans text-xs text-foreground focus:border-accent/60 focus:outline-none" @change="setProj('env_file', ($event.target as HTMLInputElement).value)" />
        </label>
        <p class="m-0 text-[11px] leading-relaxed text-muted-foreground/70">Saved to <code>.burrow/config.toml</code> and applied when a chat provider starts in this project.</p>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { PhX, PhPlus } from "@phosphor-icons/vue";
import { useChatAgentsStore, transportLabel as transportLabelFor, type ChatTransport } from "@/stores/chatAgents";
import { agentIconComp, AGENT_ICON_KEYS } from "@/lib/agentIcons";
import { useScriptsStore, type ProjectSettings } from "@/stores/scripts";
import { eventToShortcut } from "@/lib/shortcuts";

const props = defineProps<{ cwd?: string }>();
defineEmits<{ close: [] }>();

const chatAgents = useChatAgentsStore();
const scriptsStore = useScriptsStore();

const selectedId = ref(chatAgents.agents[0]?.id ?? "claude");
const agent = computed(() => chatAgents.agents.find((a) => a.id === selectedId.value));
const transportLabel = (transport: ChatTransport) => transportLabelFor(transport);

// Env editor rows mirror agent.env; committed back on edit.
const envRows = ref<{ k: string; v: string }[]>([]);
watch(agent, (a) => { envRows.value = a ? Object.entries(a.env).map(([k, v]) => ({ k, v })) : []; }, { immediate: true });

function commitEnv() {
  if (!agent.value) return;
  const env: Record<string, string> = {};
  for (const r of envRows.value) { if (r.k.trim()) env[r.k.trim()] = r.v; }
  agent.value.env = env;
}
function addEnv() { envRows.value.push({ k: "", v: "" }); }
function removeEnv(i: number) { envRows.value.splice(i, 1); commitEnv(); }
function setArgs(v: string) { if (agent.value) agent.value.args = v.split(/\s+/).filter(Boolean); }

const recordingId = ref<string | null>(null);
function startRecording(id: string, e: MouseEvent) {
  recordingId.value = recordingId.value === id ? null : id;
  if (recordingId.value === id) (e.currentTarget as HTMLElement)?.focus();
}
function onRecordKey(id: string, e: KeyboardEvent) {
  if (recordingId.value !== id) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") { recordingId.value = null; return; }
  const sc = eventToShortcut(e);
  if (!sc) return;
  const a = chatAgents.agents.find((x) => x.id === id);
  if (a) a.shortcut = sc;
  recordingId.value = null;
}

function onAdd() { selectedId.value = chatAgents.add().id; }
function onDelete() { const id = selectedId.value; chatAgents.remove(id); selectedId.value = chatAgents.agents[0]?.id ?? "claude"; }
function onReset() { chatAgents.reset(selectedId.value); }

// Per-project settings
const proj = computed<ProjectSettings>(() => scriptsStore.settingsFor(props.cwd));
const cwdShort = computed(() => props.cwd?.split("/").slice(-2).join("/") ?? "");
function setProj(key: keyof ProjectSettings, val: string) {
  if (!props.cwd) return;
  scriptsStore.updateSettings(props.cwd, { [key]: val.trim() || undefined });
}
</script>
