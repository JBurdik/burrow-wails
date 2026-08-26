<template>
  <div class="mp">
    <button ref="btnEl" class="mp-trigger" type="button" @click.stop="toggle">
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
        <div class="mp-rail">
          <button
            class="mp-rail-btn"
            :class="{ active: tab === 'fav' }"
            title="Favorites"
            @click="tab = 'fav'"
          ><PhStar :size="15" weight="fill" /></button>
          <button
            v-for="a in providers"
            :key="a.id"
            class="mp-rail-btn"
            :class="{ active: tab === a.id }"
            :title="a.name"
            @click="tab = a.id"
          ><component :is="agentIconComp(a.icon)" :size="15" /></button>
        </div>

        <div class="mp-main">
          <div class="mp-search">
            <PhMagnifyingGlass :size="12" />
            <input ref="searchEl" v-model="query" placeholder="Search models…" spellcheck="false" />
          </div>
          <div class="mp-list hide-scrollbar">
            <button
              v-for="(row, i) in rows"
              :key="row.agentId + '/' + row.id"
              class="mp-row"
              :class="{ active: row.agentId === agentId && row.id === modelId }"
              @click="pick(row.agentId, row.id)"
            >
              <span class="mp-row-main">
                <span class="mp-row-label">{{ row.label }}</span>
                <span class="mp-row-sub">
                  <component :is="agentIconComp(agentById(row.agentId).icon)" :size="10" />
                  {{ agentById(row.agentId).name }}
                </span>
              </span>
              <span v-if="tab === 'fav' && i < 9" class="mp-kbd">⌘{{ i + 1 }}</span>
              <span
                class="mp-star"
                :class="{ on: isFavorite(row.agentId, row.id) }"
                @click.stop="toggleFavorite(row.agentId, row.id)"
              ><PhStar :size="12" :weight="isFavorite(row.agentId, row.id) ? 'fill' : 'regular'" /></span>
            </button>
            <div v-if="!rows.length" class="mp-empty">No models</div>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onBeforeUnmount, watch } from "vue";
import { PhCaretDown, PhStar, PhMagnifyingGlass } from "@phosphor-icons/vue";
import { useChatAgentsStore } from "@/stores/chatAgents";
import { agentIconComp } from "@/lib/agentIcons";
import { modelsFor, modelLabel, favorites, parseFav, isFavorite, toggleFavorite, ensureModels, type ModelEntry } from "@/lib/chatModels";

const props = defineProps<{
  agentId: string;
  modelId: string;
  /** Chat already running under one agent — only its own models are selectable. */
  lockAgent?: boolean;
  /** Models the running agent reported itself; overrides the static catalog. */
  models?: ModelEntry[];
  /** Working dir used when probing a CLI for its model catalog. */
  cwd?: string;
}>();
const emit = defineEmits<{ (e: "select", agentId: string, modelId: string): void }>();

const chatAgents = useChatAgentsStore();
const agentById = (id: string) => chatAgents.byId(id);
const agent = computed(() => chatAgents.byId(props.agentId));
const providers = computed(() => (props.lockAgent ? [agent.value] : chatAgents.agents));

// Provider-default models have no name of their own — show the agent instead.
const triggerLabel = computed(() => {
  if (!props.modelId) return agent.value.name;
  return props.models?.find((m) => m.id === props.modelId)?.label ?? modelLabel(props.agentId, props.modelId);
});

// Runtime-reported models only describe the CURRENT agent; other providers in
// the rail still come from the catalog.
function listFor(agentId: string): ModelEntry[] {
  if (props.models?.length && agentId === props.agentId) return props.models;
  return modelsFor(agentId);
}

const open = ref(false);
const tab = ref<string>("fav");
const query = ref("");
const btnEl = ref<HTMLElement>();
const panelEl = ref<HTMLElement>();
const searchEl = ref<HTMLInputElement>();
const pos = ref({ left: 0, bottom: 0 });

interface Row { agentId: string; id: string; label: string }

const favRows = computed<Row[]>(() =>
  favorites.value
    .map(parseFav)
    .filter((f) => !props.lockAgent || f.agentId === props.agentId)
    .filter((f) => chatAgents.agents.some((a) => a.id === f.agentId))
    .map((f) => ({
      agentId: f.agentId,
      id: f.modelId,
      label: listFor(f.agentId).find((m) => m.id === f.modelId)?.label ?? modelLabel(f.agentId, f.modelId),
    })),
);

const rows = computed<Row[]>(() => {
  const base = tab.value === "fav"
    ? favRows.value
    : listFor(tab.value).map((m) => ({ agentId: tab.value, id: m.id, label: m.label }));
  const q = query.value.trim().toLowerCase();
  return q ? base.filter((r) => r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q)) : base;
});

function toggle() {
  open.value ? close() : show();
}

function show() {
  const r = btnEl.value?.getBoundingClientRect();
  if (r) {
    pos.value = {
      left: Math.min(Math.max(8, r.left), window.innerWidth - 372),
      bottom: Math.max(8, window.innerHeight - r.top + 6),
    };
  }
  tab.value = favRows.value.length ? "fav" : props.agentId;
  query.value = "";
  // Fill in catalogs we don't know statically (Codex answers model/list).
  for (const a of providers.value) void ensureModels(a.id, a.kind, props.cwd ?? "");
  open.value = true;
  window.addEventListener("mousedown", onOutside, true);
  window.addEventListener("keydown", onKey, true);
  nextTick(() => searchEl.value?.focus());
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

// ⌘1-9 jump straight to a favourite, but only while the popover is open — the
// app already owns global ⌘1-9 for workspace switching.
function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") { close(); return; }
  if (!(e.metaKey || e.ctrlKey)) return;
  const n = Number(e.key);
  if (!n || n > 9) return;
  const row = favRows.value[n - 1];
  if (!row) return;
  e.preventDefault();
  pick(row.agentId, row.id);
}

function pick(agentId: string, modelId: string) {
  emit("select", agentId, modelId);
  close();
}

watch(() => props.lockAgent, () => close());
onBeforeUnmount(close);
</script>

<style scoped>
.mp { display: inline-flex; }

.mp-trigger {
  display: flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  background: none;
  border: none;
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 11px;
  font-weight: 500;
  padding: 5px 7px;
}
.mp-trigger:hover { color: var(--text-primary); background: var(--bg-hover); }

.mp-panel {
  position: fixed;
  z-index: 4000;
  display: flex;
  width: 364px;
  height: 300px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.6);
  overflow: hidden;
}

.mp-rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 8px 6px;
  border-right: 1px solid var(--border);
  background: var(--bg-base);
}
.mp-rail-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 7px;
  background: none;
  color: var(--text-muted);
  cursor: pointer;
}
.mp-rail-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
.mp-rail-btn.active { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }

.mp-main { flex: 1; display: flex; flex-direction: column; min-width: 0; }

.mp-search {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 11px;
  border-bottom: 1px solid var(--border);
  color: var(--text-muted);
}
.mp-search input {
  flex: 1;
  min-width: 0;
  background: none;
  border: none;
  outline: none;
  color: var(--text-primary);
  font-size: 12px;
  font-family: var(--font-ui);
}
.mp-search input::placeholder { color: var(--text-muted); }

.mp-list { flex: 1; overflow-y: auto; padding: 5px; }

.mp-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  border-radius: 8px;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 7px 8px;
}
.mp-row:hover { background: var(--bg-hover); }
.mp-row.active { background: color-mix(in srgb, var(--accent) 12%, transparent); }
.mp-row-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.mp-row-label { font-size: 12.5px; color: var(--text-primary); }
.mp-row-sub { display: flex; align-items: center; gap: 4px; font-size: 10.5px; color: var(--text-muted); }

.mp-kbd {
  flex-shrink: 0;
  font-size: 10px;
  color: var(--text-muted);
  background: var(--bg-base);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 1px 4px;
}

.mp-star { flex-shrink: 0; display: flex; color: var(--text-muted); opacity: 0.45; }
.mp-star:hover { opacity: 1; color: var(--text-primary); }
.mp-star.on { opacity: 1; color: #eab308; }

.mp-empty { padding: 16px 10px; font-size: 11.5px; color: var(--text-muted); text-align: center; }
</style>
