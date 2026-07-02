<template>
  <div class="agent-config-overlay" @mousedown.self="$emit('close')">
    <div class="agent-config">
      <header class="ac-header">
        <h2>Chat agents</h2>
        <button class="ac-x" @click="$emit('close')"><PhX :size="16" /></button>
      </header>

      <div class="ac-body">
        <!-- Agent list -->
        <aside class="ac-list">
          <button
            v-for="a in chatAgents.agents"
            :key="a.id"
            class="ac-list-item"
            :class="{ active: a.id === selectedId }"
            @click="selectedId = a.id"
          >
            <component :is="agentIconComp(a.icon)" :size="13" :style="{ color: a.color }" />
            <span>{{ a.name }}</span>
            <span class="ac-list-badge">{{ a.transport === 'acp' ? 'ACP' : 'native' }}</span>
          </button>
          <button class="ac-add" @click="onAdd"><PhPlus :size="12" /> New agent</button>
        </aside>

        <!-- Editor -->
        <section v-if="agent" class="ac-editor">
          <label class="ac-field">
            <span>Name</span>
            <input v-model="agent.name" type="text" />
          </label>

          <div class="ac-field">
            <span>Icon &amp; color</span>
            <div class="ac-icon-row">
              <button
                v-for="key in AGENT_ICON_KEYS"
                :key="key"
                class="ac-icon-btn"
                :class="{ active: agent.icon === key }"
                :style="{ color: agent.color }"
                :title="key"
                @click="agent.icon = key"
              >
                <component :is="agentIconComp(key)" :size="16" />
              </button>
              <input v-model="agent.color" type="color" class="ac-color" title="Accent color" />
            </div>
          </div>

          <div class="ac-field">
            <span>Launch shortcut</span>
            <div class="ac-shortcut-row">
              <button
                class="ac-kbd-rec"
                :class="{ recording: recordingId === agent.id }"
                :title="recordingId === agent.id ? 'Press keys… (Esc to cancel)' : 'Click to set shortcut — always opens a new chat'"
                @click="startRecording(agent.id, $event)"
                @keydown="onRecordKey(agent.id, $event)"
                @blur="recordingId === agent.id && (recordingId = null)"
              >
                {{ recordingId === agent.id ? "Press…" : (agent.shortcut || "—") }}
              </button>
              <button v-if="agent.shortcut && recordingId !== agent.id" class="ac-kbd-clear" title="Clear shortcut" @click.stop="agent.shortcut = ''">
                <PhX :size="11" />
              </button>
            </div>
          </div>

          <label class="ac-field">
            <span>Transport</span>
            <select v-model="agent.transport">
              <option value="acp">ACP (any ACP CLI)</option>
              <option value="stream-json">native (Claude stream-json)</option>
            </select>
          </label>

          <template v-if="agent.transport === 'acp'">
            <label class="ac-field">
              <span>Command</span>
              <input v-model="agent.command" type="text" placeholder="npx | gemini | codex | opencode" />
            </label>
            <label class="ac-field">
              <span>Args</span>
              <input :value="agent.args.join(' ')" type="text" placeholder="@scope/pkg --flag" @input="setArgs(($event.target as HTMLInputElement).value)" />
            </label>
            <label class="ac-field">
              <span>Kind</span>
              <select v-model="agent.kind">
                <option value="custom">custom (no special env)</option>
                <option value="claude">claude (CLAUDE_CODE_EXECUTABLE, OAuth)</option>
                <option value="gemini">gemini</option>
                <option value="codex">codex (forward API keys)</option>
              </select>
            </label>

            <div class="ac-env">
              <div class="ac-env-head"><span>Environment variables</span><button class="ac-env-add" @click="addEnv"><PhPlus :size="11" /> add</button></div>
              <div v-for="(row, i) in envRows" :key="i" class="ac-env-row">
                <input v-model="row.k" type="text" placeholder="KEY" @input="commitEnv" />
                <input v-model="row.v" type="text" placeholder="value" @input="commitEnv" />
                <button class="ac-env-del" @click="removeEnv(i)"><PhX :size="11" /></button>
              </div>
            </div>
          </template>
          <p v-else class="ac-hint">Native agents use the built-in Claude CLI transport — command/args/env are managed by Claude profiles, not here.</p>

          <div class="ac-actions">
            <button v-if="agent.builtin" class="ac-btn" @click="onReset">Reset to default</button>
            <button v-else class="ac-btn ac-btn-danger" @click="onDelete">Delete</button>
          </div>
        </section>
      </div>

      <!-- Per-project overrides (only when opened from a project chat) -->
      <section v-if="cwd" class="ac-project">
        <h3>This project <code class="ac-path">{{ cwdShort }}</code></h3>
        <label class="ac-field">
          <span>CLAUDE_CONFIG_DIR</span>
          <input :value="proj.claude_config_dir ?? ''" type="text" placeholder="(default)" @change="setProj('claude_config_dir', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="ac-field">
          <span>.env file</span>
          <input :value="proj.env_file ?? ''" type="text" placeholder=".env" @change="setProj('env_file', ($event.target as HTMLInputElement).value)" />
        </label>
        <p class="ac-hint">Saved to <code>.burrow/config.toml</code> — applied to ACP agents launched in this project.</p>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { PhX, PhPlus } from "@phosphor-icons/vue";
import { useChatAgentsStore } from "@/stores/chatAgents";
import { agentIconComp, AGENT_ICON_KEYS } from "@/lib/agentIcons";
import { useScriptsStore, type ProjectSettings } from "@/stores/scripts";
import { eventToShortcut } from "@/lib/shortcuts";

const props = defineProps<{ cwd?: string }>();
defineEmits<{ close: [] }>();

const chatAgents = useChatAgentsStore();
const scriptsStore = useScriptsStore();

const selectedId = ref(chatAgents.agents[0]?.id ?? "claude");
const agent = computed(() => chatAgents.agents.find((a) => a.id === selectedId.value));

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

<style scoped>
.agent-config-overlay { position: fixed; inset: 0; z-index: 2000; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.5); backdrop-filter: blur(2px); }
.agent-config { width: 720px; max-width: 92vw; max-height: 86vh; overflow: auto; background: #16161d; border: 1px solid rgba(255,255,255,0.1); border-radius: 14px; box-shadow: 0 20px 60px rgba(0,0,0,0.6); color: rgba(255,255,255,0.85); }
.ac-header { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; border-bottom: 1px solid rgba(255,255,255,0.08); }
.ac-header h2 { margin: 0; font-size: 15px; font-weight: 600; }
.ac-x { background: none; border: none; color: rgba(255,255,255,0.5); cursor: pointer; padding: 4px; border-radius: 6px; }
.ac-x:hover { color: #fff; background: rgba(255,255,255,0.08); }
.ac-body { display: flex; gap: 0; }
.ac-list { width: 200px; flex-shrink: 0; padding: 10px; border-right: 1px solid rgba(255,255,255,0.08); display: flex; flex-direction: column; gap: 3px; }
.ac-list-item { display: flex; align-items: center; gap: 7px; padding: 7px 9px; background: none; border: none; border-radius: 7px; color: rgba(255,255,255,0.75); font-size: 12px; cursor: pointer; text-align: left; }
.ac-list-item span:first-of-type { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ac-list-item:hover { background: rgba(255,255,255,0.05); }
.ac-list-item.active { background: rgba(124,58,237,0.18); color: #fff; }
.ac-list-badge { font-size: 8px; font-family: var(--font-mono); color: rgba(255,255,255,0.35); }
.ac-add { margin-top: 6px; display: flex; align-items: center; justify-content: center; gap: 5px; padding: 7px; background: rgba(255,255,255,0.04); border: 1px dashed rgba(255,255,255,0.15); border-radius: 7px; color: rgba(255,255,255,0.6); font-size: 11px; cursor: pointer; }
.ac-add:hover { background: rgba(255,255,255,0.08); }
.ac-editor { flex: 1; padding: 16px 18px; display: flex; flex-direction: column; gap: 12px; min-width: 0; }
.ac-field { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: rgba(255,255,255,0.55); }
.ac-field input, .ac-field select { background: #0e0e13; border: 1px solid rgba(255,255,255,0.12); border-radius: 7px; padding: 7px 9px; color: #fff; font-size: 12px; font-family: inherit; }
.ac-field input:focus, .ac-field select:focus { outline: none; border-color: rgba(124,58,237,0.6); }
.ac-icon-row { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; }
.ac-icon-btn { display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 30px; background: #0e0e13; border: 1px solid rgba(255,255,255,0.12); border-radius: 7px; cursor: pointer; }
.ac-icon-btn:hover { background: rgba(255,255,255,0.06); }
.ac-icon-btn.active { border-color: currentColor; background: rgba(255,255,255,0.06); }
.ac-shortcut-row { display: flex; align-items: center; gap: 5px; }
.ac-kbd-rec { display: inline-flex; align-items: center; justify-content: center; background: #0e0e13; border: 1px solid rgba(255,255,255,0.12); border-radius: 7px; padding: 6px 10px; min-width: 60px; color: #777; font-family: var(--font-ui); font-size: 11px; cursor: pointer; }
.ac-kbd-rec:hover { color: #aaa; border-color: rgba(255,255,255,0.25); }
.ac-kbd-rec.recording { color: #a78bfa; border-color: #7c3aed66; background: #7c3aed14; }
.ac-kbd-clear { display: flex; align-items: center; justify-content: center; background: none; border: none; color: #444; cursor: pointer; padding: 2px; border-radius: 3px; flex-shrink: 0; }
.ac-kbd-clear:hover { color: #ef4444; background: rgba(239,68,68,0.12); }
.ac-color { width: 34px; height: 30px; padding: 2px; background: #0e0e13; border: 1px solid rgba(255,255,255,0.12); border-radius: 7px; cursor: pointer; margin-left: 4px; }
.ac-env { display: flex; flex-direction: column; gap: 5px; }
.ac-env-head { display: flex; align-items: center; justify-content: space-between; font-size: 11px; color: rgba(255,255,255,0.55); }
.ac-env-add, .ac-env-del { display: inline-flex; align-items: center; gap: 3px; background: none; border: none; color: rgba(255,255,255,0.5); cursor: pointer; font-size: 11px; padding: 3px; border-radius: 5px; }
.ac-env-add:hover, .ac-env-del:hover { color: #fff; background: rgba(255,255,255,0.08); }
.ac-env-row { display: grid; grid-template-columns: 1fr 1.4fr auto; gap: 6px; }
.ac-env-row input { background: #0e0e13; border: 1px solid rgba(255,255,255,0.12); border-radius: 6px; padding: 5px 8px; color: #fff; font-size: 11px; font-family: var(--font-mono); }
.ac-hint { font-size: 11px; color: rgba(255,255,255,0.4); line-height: 1.5; margin: 0; }
.ac-actions { margin-top: auto; padding-top: 8px; }
.ac-btn { padding: 6px 12px; background: rgba(255,255,255,0.06); border: 1px solid rgba(255,255,255,0.12); border-radius: 7px; color: rgba(255,255,255,0.8); font-size: 12px; cursor: pointer; }
.ac-btn:hover { background: rgba(255,255,255,0.1); }
.ac-btn-danger { color: #f87171; border-color: rgba(248,113,113,0.3); }
.ac-btn-danger:hover { background: rgba(248,113,113,0.12); }
.ac-project { padding: 14px 18px; border-top: 1px solid rgba(255,255,255,0.08); display: flex; flex-direction: column; gap: 10px; }
.ac-project h3 { margin: 0; font-size: 12px; font-weight: 600; color: rgba(255,255,255,0.7); display: flex; align-items: center; gap: 8px; }
.ac-path { font-size: 10px; font-family: var(--font-mono); color: rgba(255,255,255,0.4); }
</style>
