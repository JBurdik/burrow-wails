<template>
  <Teleport to="body">
    <Transition name="spotlight">
      <div v-if="isOpen" class="fixed inset-0 z-[9000] flex justify-center bg-black/60 pt-[165px]" @mousedown.self="close">
        <div class="s-modal flex max-h-[600px] w-[680px] flex-col self-start overflow-hidden rounded-xl border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.55)] [backdrop-filter:var(--blur-overlay,none)]">
          <div class="flex h-14 shrink-0 items-center gap-3 border-b border-border px-4">
            <PhMagnifyingGlass :size="17" class="shrink-0 text-accent" />
            <input
              ref="inputRef"
              v-model="query"
              :placeholder="projectOnly ? 'Search projects…' : 'Search projects, agents, commands, files…'"
              class="min-w-0 flex-1 border-0 bg-transparent font-sans text-[15px] text-foreground outline-none placeholder:text-muted-foreground/50 [caret-color:var(--accent)]"
              spellcheck="false"
              autocomplete="off"
              @keydown.esc.prevent="close"
              @keydown.enter.prevent="activate"
              @keydown.up.prevent="move(-1)"
              @keydown.down.prevent="move(1)"
            />
            <kbd class="shrink-0 rounded border border-border bg-hover px-2 py-0.5 font-mono text-[11px] text-muted-foreground">esc</kbd>
          </div>

          <div ref="resultsRef" class="s-results flex-1 overflow-y-auto py-1.5">
            <template v-for="(section, si) in sections" :key="section.key">
              <div class="flex h-[26px] items-center px-4 font-sans text-[10px] font-semibold tracking-[0.06em] text-muted-foreground/60">{{ section.label }}</div>
              <div
                v-for="item in section.items"
                :key="item.id"
                :data-id="item.id"
                class="mx-1 flex h-[46px] cursor-pointer items-center gap-3 rounded-md px-3 transition-colors duration-75"
                :class="selectedId === item.id ? 'bg-selected' : 'hover:bg-hover'"
                @mouseenter="selectedId = item.id"
                @click="item.action()"
              >
                <div
                  class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[7px] border"
                  :style="{ background: tint(item.color), borderColor: `${item.color}33` }"
                >
                  <component :is="item.icon" :size="14" :color="item.color" />
                </div>
                <div class="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span class="truncate font-sans text-[13px] font-medium text-foreground">{{ item.title }}</span>
                  <span v-if="item.desc" class="truncate font-mono text-[11px] text-muted-foreground/70">{{ item.desc }}</span>
                </div>
                <span v-if="item.badge" class="shrink-0 rounded border border-accent/25 bg-accent/10 px-1.5 py-0.5 font-sans text-[10px] text-accent">{{ item.badge }}</span>
                <kbd
                  v-if="item.shortcut"
                  class="shrink-0 rounded border border-border bg-hover px-2 py-0.5 font-mono text-[11px] text-muted-foreground"
                >{{ item.shortcut }}</kbd>
              </div>
              <div v-if="si < sections.length - 1" class="my-1 h-px bg-border/60" />
            </template>
            <div v-if="!sections.length" class="px-4 py-6 text-center font-sans text-[12px] text-muted-foreground/60">
              No matches
            </div>
          </div>

          <div class="flex h-9 shrink-0 items-center gap-4 border-t border-border bg-base px-4 text-[11px] text-muted-foreground/70">
            <span><kbd class="s-key">↑↓</kbd> navigate</span>
            <span><kbd class="s-key">↵</kbd> run</span>
            <div class="flex-1" />
            <span class="truncate font-mono text-muted-foreground/50">{{ wsStore.active?.name ?? "no project" }}</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import {
  PhTerminal, PhSparkle, PhCode, PhGitBranch, PhRobot,
  PhFolderOpen, PhGear, PhPlus, PhColumns, PhPalette, PhKeyboard, PhGlobe, PhPlayCircle,
  PhFileText, PhMagnifyingGlass, PhArrowsClockwise,
} from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useProvidersStore } from "@/stores/providers";
import { useScriptsStore } from "@/stores/scripts";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useKeybindingsStore } from "@/stores/keybindings";
import { pickDir } from "@/lib/pickPath";
import type { Component } from "vue";

const emit = defineEmits<{
  launch: [cmd: string];
  newTerminal: [];
  openProjectConfig: [];
  openBrowser: [];
  repaint: [];
  toggleManager: [];
  splitTerminal: [];
  openFile: [path: string, line: number];
}>();

const providers = useProvidersStore();
const scriptsStore = useScriptsStore();
const wsStore = useWorkspaceStore();
const keys = useKeybindingsStore();
const ui = useUIStore();

const isOpen = ref(false);
const query = ref("");
const selectedId = ref("");
const inputRef = ref<HTMLInputElement | null>(null);
const resultsRef = ref<HTMLElement | null>(null);

// ⌘⇧O / Sidebar "New chat" open the palette scoped to just projects — pick
// one, land on the Welcome composer for it instead of jumping into a chat tab.
const projectOnly = ref(false);

// --- workspace search: rg over the active project, debounced per keystroke.
// ponytail: no index, no cache — one shell-out is faster than keeping an index
// honest, and results die with the palette anyway.
interface SearchHit { path: string; line: number; text: string }
const hits = ref<SearchHit[]>([]);
let searchTimer: number | undefined;
let searchSeq = 0;

watch(query, (q) => {
  clearTimeout(searchTimer);
  const term = q.trim();
  const cwd = wsStore.active?.path;
  if (term.length < 2 || !cwd || projectOnly.value) return (hits.value = []);
  const seq = ++searchSeq;
  searchTimer = window.setTimeout(async () => {
    const found = await invoke<SearchHit[]>("search_files", { cwd, query: term, limit: 24 }).catch(() => []);
    // Drop a slow reply whose query the user has already typed past.
    if (seq === searchSeq) hits.value = found;
  }, 140);
});

const ICON_MAP: Record<string, Component> = {
  sparkle: PhSparkle,
  code: PhCode,
  "git-branch": PhGitBranch,
  robot: PhRobot,
  terminal: PhTerminal,
};

// Icon-chip background: the accent colour at ~11% over the panel.
function tint(hex: string): string {
  if (!hex.startsWith("#") || hex.length < 7) return "var(--bg-hover)";
  return `${hex}1f`;
}

interface SpotlightItem {
  id: string;
  title: string;
  desc?: string;
  icon: Component;
  color: string;
  shortcut?: string;
  badge?: string;
  action: () => void;
}

const MUTED = "#8b8b8b";
const q = computed(() => query.value.toLowerCase().trim());
const hasQuery = computed(() => q.value.length > 0);

function hit(...fields: (string | undefined)[]) {
  return !hasQuery.value || fields.some((f) => f?.toLowerCase().includes(q.value));
}

// Projects, most useful first: the active one, then by last opened. The active
// project heads the list so ⌘⇧O → ↵ starts a thread where you already are.
const projectItems = computed<SpotlightItem[]>(() => {
  const activeId = wsStore.active?.id;
  return [...wsStore.topLevel]
    .sort((a, b) => {
      if (a.id === activeId) return -1;
      if (b.id === activeId) return 1;
      return (b.last_opened ?? 0) - (a.last_opened ?? 0);
    })
    .filter((w) => hit(w.name, w.path))
    .map((w) => ({
      id: `ws-${w.id}`,
      title: w.name,
      desc: w.path,
      icon: PhFolderOpen as Component,
      color: "#a78bfa",
      badge: w.id === activeId ? "current" : undefined,
      action: () => {
        wsStore.open(w);
        close();
        if (projectOnly.value) ui.openWelcome();
      },
    }));
});

const agentItems = computed<SpotlightItem[]>(() =>
  providers.active
    .filter((a) => providers.commandLine(a) && hit(a.name, providers.commandLine(a)))
    .map((a) => ({
      id: `agent-${a.id}`,
      title: `Run ${a.name}`,
      desc: providers.commandLine(a),
      icon: ICON_MAP[a.icon] ?? PhRobot,
      color: a.color,
      shortcut: a.terminalShortcut || undefined,
      action: () => { emit("launch", providers.commandLine(a)); close(); },
    })),
);

const scriptItems = computed<SpotlightItem[]>(() =>
  scriptsStore.scriptsFor(wsStore.active?.path)
    .filter((s) => scriptsStore.commandLine(s) && hit(s.name, scriptsStore.commandLine(s)))
    .map((s) => ({
      id: `script-${s.id}`,
      title: `Run ${s.name}`,
      desc: scriptsStore.commandLine(s),
      icon: PhPlayCircle as Component,
      color: s.color || "#34d399",
      action: () => { emit("launch", scriptsStore.commandLine(s)); close(); },
    })),
);

// Commands carry their live binding from the keybindings store, so what the
// palette prints is always what actually fires (incl. user rebinds).
const commandItems = computed<SpotlightItem[]>(() => {
  const defs: { id: string; title: string; icon: Component; color: string; keyId?: string; action: () => void }[] = [
    { id: "cmd-newterm", title: "New Terminal Tab", icon: PhTerminal as Component, color: "#34d399", keyId: "newTab", action: () => { emit("newTerminal"); close(); } },
    { id: "cmd-split", title: "Split Terminal", icon: PhColumns as Component, color: "#34d399", keyId: "splitH", action: () => { emit("splitTerminal"); close(); } },
    { id: "cmd-browser", title: "New Browser Tab", icon: PhGlobe as Component, color: "#60a5fa", action: () => { emit("openBrowser"); close(); } },
    { id: "cmd-manager", title: "Toggle Manager", icon: PhSparkle as Component, color: "#ec4899", keyId: "manager", action: () => { emit("toggleManager"); close(); } },
    { id: "cmd-new-project", title: "New Project…", icon: PhPlus as Component, color: "#a78bfa", keyId: "newProject", action: newProject },
    { id: "cmd-project-config", title: "Project Settings…", icon: PhGear as Component, color: MUTED, action: () => { emit("openProjectConfig"); close(); } },
    { id: "cmd-settings", title: "Settings", icon: PhGear as Component, color: MUTED, keyId: "settings", action: () => { openSettingsAt("general"); } },
    { id: "cmd-theme", title: "Change Theme", icon: PhPalette as Component, color: "#fbbf24", action: () => { openSettingsAt("appearance"); } },
    { id: "cmd-keys", title: "Keyboard Shortcuts", icon: PhKeyboard as Component, color: MUTED, keyId: "cheatsheet", action: () => { openSettingsAt("keybindings"); } },
    { id: "cmd-repaint", title: "Repaint Terminals (un-scramble)", icon: PhArrowsClockwise as Component, color: "#fbbf24", keyId: "repaint", action: () => { emit("repaint"); close(); } },
  ];
  return defs
    .filter((c) => hit(c.title))
    .map((c) => ({ ...c, shortcut: c.keyId ? keys.shortcut(c.keyId) || undefined : undefined }));
});

const fileItems = computed<SpotlightItem[]>(() =>
  hits.value.map((h, i) => ({
    id: `hit-${i}`,
    title: h.line ? `${h.path}:${h.line}` : h.path,
    desc: h.text || undefined,
    icon: (h.line ? PhMagnifyingGlass : PhFileText) as Component,
    color: "#60a5fa",
    action: () => { emit("openFile", h.path, h.line); close(); },
  })),
);

// Empty query = context only (this project's agents + your projects). Commands,
// scripts and file hits appear once you type — that's what killed the wall of
// unrelated rows the palette used to open with.
const sections = computed(() => {
  if (projectOnly.value) {
    return [{ key: "projects", label: "PROJECTS", items: projectItems.value }]
      .filter((s) => s.items.length);
  }
  const all = hasQuery.value
    ? [
        { key: "agents", label: "AGENTS", items: agentItems.value },
        { key: "projects", label: "PROJECTS", items: projectItems.value },
        { key: "scripts", label: "SCRIPTS", items: scriptItems.value },
        { key: "commands", label: "COMMANDS", items: commandItems.value },
        { key: "files", label: "IN FILES", items: fileItems.value },
      ]
    : [
        { key: "agents", label: "RUN AGENT", items: agentItems.value.slice(0, 5) },
        { key: "projects", label: "PROJECTS", items: projectItems.value.slice(0, 5) },
      ];
  return all.filter((s) => s.items.length);
});

const flatItems = computed(() => sections.value.flatMap((s) => s.items));

function selectFirst() {
  selectedId.value = flatItems.value[0]?.id ?? "";
}

watch([query, sections], () => {
  if (!flatItems.value.some((i) => i.id === selectedId.value)) selectFirst();
});

function move(dir: 1 | -1) {
  const items = flatItems.value;
  const idx = items.findIndex((i) => i.id === selectedId.value);
  const next = Math.max(0, Math.min(items.length - 1, idx + dir));
  selectedId.value = items[next]?.id ?? "";
  // Keyboard-only: hovering also sets selectedId, and scrolling there would
  // fight the pointer.
  nextTick(() => {
    resultsRef.value
      ?.querySelector(`[data-id="${CSS.escape(selectedId.value)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  });
}

function activate() {
  flatItems.value.find((i) => i.id === selectedId.value)?.action();
}

function openSettingsAt(section: string) {
  ui.openSettings(section);
  close();
}

// Project creation goes through the in-app directory picker (PathPicker.vue) —
// no native dialog anywhere, and ⌘↵ in the picker creates the folder first.
async function newProject() {
  close();
  const path = await pickDir({ title: "Add project", start: "~/" });
  if (!path) return;
  const name = path.split("/").filter(Boolean).pop() ?? path;
  const ws = await wsStore.create(name, path);
  if (ws) wsStore.open(ws);
}

function show(opts?: { projectOnly?: boolean }) {
  isOpen.value = true;
  projectOnly.value = !!opts?.projectOnly;
  query.value = "";
  hits.value = [];
  nextTick(() => {
    inputRef.value?.focus();
    selectFirst();
  });
}

function close() {
  isOpen.value = false;
}

defineExpose({ show, close, newProject });
</script>

<style scoped>
.s-key {
  border-radius: 3px;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  padding: 1px 5px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-secondary);
}

/* Custom scrollbar (::-webkit-scrollbar has no Tailwind utility) and the
   Vue <Transition> phase classes stay as real CSS. */
.s-results::-webkit-scrollbar { width: 4px; }
.s-results::-webkit-scrollbar-track { background: transparent; }
.s-results::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

.spotlight-enter-active,
.spotlight-leave-active {
  transition: opacity 0.12s ease;
}
.spotlight-enter-active .s-modal,
.spotlight-leave-active .s-modal {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.spotlight-enter-from,
.spotlight-leave-to {
  opacity: 0;
}
.spotlight-enter-from .s-modal,
.spotlight-leave-to .s-modal {
  opacity: 0;
  transform: translateY(-8px) scale(0.98);
}
</style>
