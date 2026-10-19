<template>
  <div class="mp">
    <button ref="btnEl" class="composer-pill" type="button" @click.stop="toggle">
      <component :is="agentIconComp(agent.icon)" :size="12" />
      {{ triggerLabel }}
      <PhCaretDown :size="9" weight="bold" />
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="panelEl"
        class="mp-panel"
        :style="{ left: pos.left + 'px', bottom: pos.bottom + 'px' }"
        @click.stop
      >
        <div class="mp-tabs">
          <button
            class="mp-tab"
            :class="{ active: tab === 'fav' }"
            title="Favorites"
            @click="setTab('fav')"
          ><PhStar :size="15" weight="fill" /></button>
          <button
            v-for="a in providers"
            :key="a.id"
            class="mp-tab"
            :class="{ active: tab === a.id }"
            :title="a.name"
            @click="setTab(a.id)"
          ><component :is="agentIconComp(a.icon)" :size="15" /></button>
        </div>

        <div ref="listEl" class="mp-list hide-scrollbar">
          <button
            v-for="(row, i) in rows"
            :key="row.agentId + '/' + row.id"
            class="mp-row"
            :class="{ cursor: i === cursor, current: row.agentId === agentId && row.id === modelId }"
            @mousemove="cursor = i"
            @click="pick(row)"
          >
            <component v-if="tab === 'fav'" :is="agentIconComp(agentById(row.agentId).icon)" :size="12" class="mp-row-icon" />
            <span class="mp-row-label">{{ row.label }}</span>
            <PhCheck v-if="row.agentId === agentId && row.id === modelId" :size="12" weight="bold" class="mp-row-check" />
            <span
              class="mp-star"
              :class="{ on: isFavorite(row.agentId, row.id) }"
              :title="isFavorite(row.agentId, row.id) ? 'Remove from favorites' : 'Add to favorites'"
              @click.stop="toggleFavorite(row.agentId, row.id)"
            ><PhStar :size="13" :weight="isFavorite(row.agentId, row.id) ? 'fill' : 'regular'" /></span>
          </button>
          <div v-if="!rows.length" class="mp-empty">{{ tab === 'fav' ? 'Star a model to pin it here' : 'No models' }}</div>
        </div>

        <!-- Only what the active transport supports — the parent decides. -->
        <div v-if="settings?.length" class="mp-settings">
          <template v-for="s in settings" :key="s.key">
            <button
              v-if="s.toggle"
              class="mp-set mp-set-toggle"
              :class="{ on: s.active === s.items[1]?.id }"
              @click="emit('setting', s.key, (s.active === s.items[1]?.id ? s.items[0] : s.items[1]).id)"
            >{{ s.label }}</button>
            <label v-else class="mp-set">
              <span class="mp-set-label">{{ s.label }}</span>
              <select :value="s.active" @change="emit('setting', s.key, ($event.target as HTMLSelectElement).value)">
                <option v-for="it in s.items" :key="it.id" :value="it.id">{{ it.label }}</option>
              </select>
              <PhCaretDown :size="9" weight="bold" class="mp-set-caret" />
            </label>
          </template>
        </div>

        <div class="mp-hint">
          <span><kbd>↑↓</kbd> model</span>
          <span v-if="effortSetting"><kbd>←→</kbd> effort</span>
          <span v-if="providers.length"><kbd>⇥</kbd> agent</span>
          <span><kbd>↵</kbd> this chat</span>
          <span><kbd>⌥↵</kbd> default</span>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onBeforeUnmount, watch } from "vue";
import { PhCaretDown, PhCheck, PhStar } from "@phosphor-icons/vue";
import { useProvidersStore } from "@/stores/providers";
import { useUIStore } from "@/stores/ui";
import { agentIconComp } from "@/lib/agentIcons";
import { setConfig } from "@/lib/config";
import { setLastAcpSetting } from "@/lib/acpSettings";
import { visibleModelsFor, modelLabel, favorites, parseFav, isFavorite, toggleFavorite, ensureModels, type ModelEntry } from "@/lib/chatModels";

/** One quick setting row entry. `key: "effort"` is what ←→ steps through;
 *  `toggle` renders a switch where items[0] = off, items[1] = on. */
export interface PickerSetting {
  key: string;
  label: string;
  items: { id: string; label: string }[];
  active: string;
  toggle?: boolean;
}

const props = defineProps<{
  agentId: string;
  modelId: string;
  /** Chat already running under one agent — only its own models are selectable. */
  lockAgent?: boolean;
  /** Models the running agent reported itself; overrides the static catalog. */
  models?: ModelEntry[];
  /** Working dir used when probing a CLI for its model catalog. */
  cwd?: string;
  /** The chat's quick settings (effort, plan mode, permissions…). */
  settings?: PickerSetting[];
}>();
const emit = defineEmits<{
  (e: "select", agentId: string, modelId: string): void;
  (e: "setting", key: string, value: string): void;
}>();

const chatAgents = useProvidersStore();
const ui = useUIStore();
const agentById = (id: string) => chatAgents.resolve(id);
const agent = computed(() => chatAgents.resolve(props.agentId));
const providers = computed(() => (props.lockAgent ? [agent.value] : chatAgents.chatAgents));
const effortSetting = computed(() => props.settings?.find((s) => s.key === "effort"));

// Provider-default models have no name of their own — show the agent instead.
const triggerLabel = computed(() => {
  if (!props.modelId) return agent.value.name;
  return props.models?.find((m) => m.id === props.modelId)?.label ?? modelLabel(props.agentId, props.modelId);
});

// Runtime-reported models only describe the CURRENT agent; other providers in
// the tab row still come from the catalog.
function listFor(agentId: string): ModelEntry[] {
  if (props.models?.length && agentId === props.agentId) return props.models;
  return visibleModelsFor(agentId);
}

const open = ref(false);
const tab = ref<string>("fav");
const cursor = ref(0);
const btnEl = ref<HTMLElement>();
const panelEl = ref<HTMLElement>();
const listEl = ref<HTMLElement>();
const pos = ref({ left: 0, bottom: 0 });

interface Row { agentId: string; id: string; label: string }

const favRows = computed<Row[]>(() =>
  favorites.value
    .map(parseFav)
    .filter((f) => !props.lockAgent || f.agentId === props.agentId)
    .filter((f) => chatAgents.chatAgents.some((a) => a.id === f.agentId))
    .map((f) => ({
      agentId: f.agentId,
      id: f.modelId,
      label: listFor(f.agentId).find((m) => m.id === f.modelId)?.label ?? modelLabel(f.agentId, f.modelId),
    })),
);

function rowsFor(t: string): Row[] {
  return t === "fav" ? favRows.value : listFor(t).map((m) => ({ agentId: t, id: m.id, label: m.label }));
}
const rows = computed(() => rowsFor(tab.value));
// The tab the picker opens on.
const homeTab = () => (favRows.value.length ? "fav" : props.agentId);

function setTab(t: string) {
  tab.value = t;
  const i = rows.value.findIndex((r) => r.agentId === props.agentId && r.id === props.modelId);
  cursor.value = Math.max(0, i);
}

function toggle() {
  open.value ? close() : show();
}

const PANEL_W = 340;
function show() {
  const r = btnEl.value?.getBoundingClientRect();
  if (r) {
    pos.value = {
      left: Math.min(Math.max(8, r.left), window.innerWidth - PANEL_W - 8),
      bottom: Math.max(8, window.innerHeight - r.top + 6),
    };
  }
  setTab(homeTab());
  // Fill in catalogs we don't know statically (Codex answers model/list).
  for (const a of providers.value) void ensureModels(a.id, a.kind, props.cwd ?? "");
  open.value = true;
  window.addEventListener("mousedown", onOutside, true);
  window.addEventListener("keydown", onKey, true);
}

function close() {
  open.value = false;
  window.removeEventListener("mousedown", onOutside, true);
  window.removeEventListener("keydown", onKey, true);
}

function onOutside(e: MouseEvent) {
  const t = e.target as Node;
  if (panelEl.value?.contains(t) || btnEl.value?.contains(t)) return;
  close();
}

function stepEffort(dir: number) {
  const s = effortSetting.value;
  if (!s) return;
  const i = s.items.findIndex((it) => it.id === s.active);
  const next = s.items[Math.min(s.items.length - 1, Math.max(0, i + dir))];
  if (next && next.id !== s.active) emit("setting", s.key, next.id);
}

function moveCursor(dir: number) {
  const n = rows.value.length;
  if (!n) return;
  cursor.value = (cursor.value + dir + n) % n;
  nextTick(() => listEl.value?.children[cursor.value]?.scrollIntoView({ block: "nearest" }));
}

function cycleTab(dir: number) {
  const tabs = ["fav", ...providers.value.map((a) => a.id)];
  setTab(tabs[(tabs.indexOf(tab.value) + dir + tabs.length) % tabs.length]);
}

// Captured on window while open, so the composer textarea never sees them.
function onKey(e: KeyboardEvent) {
  // A focused <select> in the settings row owns its own arrows.
  if (e.target instanceof HTMLSelectElement && e.key !== "Escape") return;
  const handled = (() => {
    switch (e.key) {
      case "Escape": close(); return true;
      case "ArrowDown": moveCursor(1); return true;
      case "ArrowUp": moveCursor(-1); return true;
      case "ArrowLeft": stepEffort(-1); return true;
      case "ArrowRight": stepEffort(1); return true;
      case "Tab": cycleTab(e.shiftKey ? -1 : 1); return true;
      case "Enter": {
        const row = rows.value[cursor.value];
        if (row) (e.altKey ? pickDefault(row) : pick(row));
        return true;
      }
    }
    return false;
  })();
  if (handled) { e.preventDefault(); e.stopPropagation(); }
}

function pick(row: Row) {
  emit("select", row.agentId, row.id);
  close();
}

// ⌥↵: make it what the NEXT new chat starts with, leave this one alone. Same
// keys the welcome composer and AgentChat seed new chats from.
function pickDefault(row: Row) {
  ui.defaultChatAgent = row.agentId;
  if (row.id) {
    if (chatAgents.resolve(row.agentId).kind === "claude") setConfig("chatLastUsedModel", row.id);
    else setLastAcpSetting(row.agentId, "model", row.id);
  }
  close();
}


watch(() => props.lockAgent, () => close());
onBeforeUnmount(close);
</script>

<style scoped>
.mp { display: inline-flex; }

.mp-panel {
  position: fixed;
  z-index: 4000;
  display: flex;
  flex-direction: column;
  width: 340px;
  background: var(--bg-dropdown);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.45);
  overflow: hidden;
  font-family: var(--font-ui);
}

.mp-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  padding: 4px 8px 0;
  border-bottom: 1px solid var(--border);
}
.mp-tab {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border: none;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
}
.mp-tab:hover { color: var(--text-primary); }
.mp-tab.active { color: var(--text-primary); border-bottom-color: var(--accent); }

.mp-list { max-height: 264px; overflow-y: auto; padding: 5px; }

.mp-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 32px;
  text-align: left;
  background: none;
  border: none;
  border-radius: 8px;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 0 8px 0 10px;
}
.mp-row.cursor { background: var(--bg-hover); }
.mp-row-icon { flex-shrink: 0; color: var(--text-muted); }
.mp-row-label { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; color: var(--text-primary); }
.mp-row.current .mp-row-label { font-weight: 600; }
.mp-row-check { flex-shrink: 0; color: var(--accent); }


.mp-star { flex-shrink: 0; display: flex; color: var(--text-muted); opacity: 0.45; }
.mp-star:hover { opacity: 1; color: var(--text-primary); }
.mp-star.on { opacity: 1; color: var(--yellow, #eab308); }

.mp-empty { padding: 18px 10px; font-size: 11.5px; color: var(--text-muted); text-align: center; }

.mp-settings {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 6px 8px;
  border-top: 1px solid var(--border);
}
.mp-set {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 8px;
  border: none;
  border-radius: 7px;
  background: none;
  color: var(--text-secondary);
  font-size: 11.5px;
  font-family: inherit;
  cursor: pointer;
}
.mp-set:hover { background: var(--bg-hover); color: var(--text-primary); }
.mp-set-label { color: var(--text-muted); }
.mp-set select {
  appearance: none;
  field-sizing: content; /* as wide as the chosen option, not the longest */
  background: none;
  border: none;
  outline: none;
  padding: 0 10px 0 0;
  color: var(--text-primary);
  font: inherit;
  cursor: pointer;
}
.mp-set-caret { position: absolute; right: 8px; pointer-events: none; color: var(--text-muted); }
.mp-set-toggle.on { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }

.mp-hint {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  padding: 7px 12px;
  border-top: 1px solid var(--border);
  font-size: 10.5px;
  color: var(--text-muted);
}
.mp-hint kbd { font-family: inherit; color: var(--text-secondary); }
</style>
