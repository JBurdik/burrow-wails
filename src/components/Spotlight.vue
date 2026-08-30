<template>
  <Teleport to="body">
    <Transition name="spotlight">
      <div v-if="isOpen" class="fixed inset-0 z-[9000] flex justify-center bg-black/60 pt-[165px]" @mousedown.self="close">
        <div class="s-modal flex max-h-[600px] w-[680px] flex-col self-start overflow-hidden rounded-xl border border-[#2a2a2a] bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.6),0_1px_0_rgba(255,255,255,0.08)] [backdrop-filter:var(--blur-overlay,none)]">
          <div class="flex h-14 shrink-0 items-center gap-3 border-b border-[#1e1e1e] px-4">
            <button v-if="browsing" class="shrink-0 text-[#666] hover:text-[#e2e2e2]" title="Back" @click="exitBrowse">
              <PhArrowLeft :size="16" />
            </button>
            <PhTerminal v-else :size="18" color="#ec4899" />
            <input
              v-if="browsing"
              ref="browseRef"
              v-model="browsePath"
              placeholder="~/code/"
              class="flex-1 border-0 bg-transparent font-mono text-[15px] text-[#e2e2e2] outline-none placeholder:text-[#444] [caret-color:#a78bfa]"
              spellcheck="false"
              autocomplete="off"
              @keydown.esc.prevent="exitBrowse"
              @keydown.enter.prevent="addProject"
              @keydown.tab.prevent="completeSelected"
              @keydown.up.prevent="move(-1)"
              @keydown.down.prevent="move(1)"
            />
            <input
              v-else
              ref="inputRef"
              v-model="query"
              placeholder="run claude --"
              class="flex-1 border-0 bg-transparent font-sans text-[15px] text-[#e2e2e2] outline-none placeholder:text-[#444] [caret-color:#ec4899]"
              spellcheck="false"
              autocomplete="off"
              @keydown.esc.prevent="close"
              @keydown.enter.prevent="activate"
              @keydown.up.prevent="move(-1)"
              @keydown.down.prevent="move(1)"
            />
            <div
              v-if="browsing"
              class="shrink-0 cursor-pointer rounded border border-[#a78bfa33] bg-[#17141f] px-2 py-0.5 text-[11px] text-[#a78bfa]"
              @click="addProject"
            >Add ↵</div>
            <div v-else class="shrink-0 rounded border border-[#2a2a2a] bg-[#1a1a1a] px-2 py-0.5 text-[11px] text-[#555]">esc</div>
          </div>

          <div class="s-results flex-1 overflow-y-auto py-1.5">
            <template v-for="(section, si) in sections" :key="section.key">
              <template v-if="section.items.length">
                <div class="flex h-[26px] items-center px-4 font-sans text-[10px] font-semibold tracking-[0.05em] text-[#3a3a3a]">{{ section.label }}</div>
                <div
                  v-for="item in section.items"
                  :key="item.id"
                  class="mx-1 flex h-[46px] cursor-pointer items-center gap-3 rounded-md px-3 transition-colors duration-75"
                  :style="{ background: selectedId === item.id ? item.iconBg : 'transparent' }"
                  @mouseenter="selectedId = item.id"
                  @click="runItem(item)"
                >
                  <div class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[7px] border border-transparent" :style="{ background: item.iconBg, borderColor: item.iconBorder }">
                    <component :is="item.icon" :size="14" :color="item.iconColor" />
                  </div>
                  <div class="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span class="truncate font-sans text-[13px] font-medium" :style="{ color: item.dim ? '#999' : '#e8e8e8' }">{{ item.title }}</span>
                    <span v-if="item.desc" class="truncate font-mono text-[11px] text-[#383838]">{{ item.desc }}</span>
                  </div>
                  <div
                    v-if="item.shortcut"
                    class="shrink-0 rounded border border-[#222] bg-[#161616] px-2 py-0.5 font-sans text-[11px] text-[#444] transition-all duration-75"
                    :style="selectedId === item.id && !item.dim
                      ? { background: item.iconBg, borderColor: item.iconBorder, color: item.iconColor }
                      : {}"
                  >{{ item.shortcut }}</div>
                </div>
                <div v-if="si < sections.length - 1" class="my-1 h-px bg-[#1a1a1a]" />
              </template>
            </template>
          </div>

          <div v-if="browsing" class="flex h-9 shrink-0 items-center gap-4 border-t border-[#1e1e1e] bg-[#0d0d0d] px-4">
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">↑↓</span><span>navigate</span></div>
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">⇥</span><span>enter dir</span></div>
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">↵</span><span>add project</span></div>
            <div class="flex-1" />
            <div class="truncate font-mono text-[11px] text-[#333]">{{ browseError || expanded }}</div>
          </div>
          <div v-else class="flex h-9 shrink-0 items-center gap-4 border-t border-[#1e1e1e] bg-[#0d0d0d] px-4">
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="s-key-sm rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">↑↓</span><span>navigate</span></div>
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="s-key-sm rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">↵</span><span>run</span></div>
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="s-key-sm rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">⌘↵</span><span>new tab</span></div>
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]"><span class="s-key-sm rounded border border-[#252525] bg-[#161616] px-1.5 py-0.5 text-[11px] text-[#555]">⇥</span><span>complete</span></div>
            <div class="flex-1" />
            <div class="flex items-center gap-1.5 font-sans text-[11px] text-[#333]">
              <PhSparkle :size="11" color="#ec4899" />
              <span>Claude Code</span>
            </div>
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
  PhFolder, PhArrowLeft, PhArrowUUpLeft, PhFileText, PhMagnifyingGlass,
} from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useAgentsStore } from "@/stores/agents";
import { useScriptsStore } from "@/stores/scripts";
import { useWorkspaceStore } from "@/stores/workspace";
import type { Component } from "vue";

const emit = defineEmits<{
  launch: [cmd: string];
  newTerminal: [];
  newWorkspace: [];
  openSettings: [];
  openProjectConfig: [];
  openBrowser: [];
  repaint: [];
  toggleManager: [];
  openFile: [path: string, line: number];
}>();

const isOpen = ref(false);
const query = ref("");
const selectedId = ref("");
const inputRef = ref<HTMLInputElement | null>(null);

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
  if (term.length < 2 || !cwd || browsing.value) return (hits.value = []);
  const seq = ++searchSeq;
  searchTimer = window.setTimeout(async () => {
    const found = await invoke<SearchHit[]>("search_files", { cwd, query: term, limit: 24 }).catch(() => []);
    // Drop a slow reply whose query the user has already typed past.
    if (seq === searchSeq) hits.value = found;
  }, 140);
});

function openHit(hit: SearchHit) {
  emit("openFile", hit.path, hit.line);
  close();
}

// --- directory browse mode (t3code-style "Add project" without a native dialog)
const browsing = ref(false);
const browsePath = ref("~/");
const browseRef = ref<HTMLInputElement | null>(null);
const browseEntries = ref<string[]>([]);
const browseError = ref("");
let home = "";

// The input holds a path; everything after the last "/" filters the listing of
// the dir before it — so typing and navigating are the same gesture.
const browseDir = computed(() => browsePath.value.slice(0, browsePath.value.lastIndexOf("/") + 1) || "~/");
const browseFilter = computed(() => browsePath.value.slice(browsePath.value.lastIndexOf("/") + 1).toLowerCase());
const expanded = computed(() => expand(browsePath.value));

function expand(p: string) {
  return p.startsWith("~") ? home + p.slice(1) : p;
}

async function loadDir(dir: string) {
  browseError.value = "";
  try {
    // The Go backend serializes `isDir`; the older Rust payload used `is_dir`.
    const entries = await invoke<{ name: string; is_dir?: boolean; isDir?: boolean }[]>("read_dir_shallow", { path: expand(dir) });
    browseEntries.value = entries
      .filter((e) => (e.is_dir ?? e.isDir) && !e.name.startsWith("."))
      .map((e) => e.name)
      .sort((a, b) => a.localeCompare(b));
  } catch (e) {
    browseEntries.value = [];
    browseError.value = String(e);
  }
}

watch(browseDir, (dir) => { if (browsing.value) loadDir(dir); });

async function enterBrowse() {
  if (!home) {
    // ponytail: no dedicated home-dir binding — derive it from the Claude config dir.
    const dirs = await invoke<{ claude: string }>("get_config_dirs").catch(() => null);
    home = dirs?.claude.replace(/\/\.claude\/?$/, "") ?? "";
  }
  browsing.value = true;
  browsePath.value = "~/";
  await loadDir("~/");
  nextTick(() => { browseRef.value?.focus(); selectFirst(); });
}

function exitBrowse() {
  browsing.value = false;
  nextTick(() => { inputRef.value?.focus(); selectFirst(); });
}

function descend(name: string) {
  browsePath.value = name === ".." ? parentOf(browseDir.value) : browseDir.value + name + "/";
  nextTick(() => { browseRef.value?.focus(); selectFirst(); });
}

function parentOf(dir: string) {
  const trimmed = dir.replace(/\/$/, "");
  const cut = trimmed.lastIndexOf("/");
  return cut <= 0 ? "/" : trimmed.slice(0, cut + 1);
}

function completeSelected() {
  const item = flatItems.value.find((i) => i.id === selectedId.value);
  if (item?.id.startsWith("dir-")) descend(item.id.slice(4));
}

async function addProject() {
  const path = expand(browsePath.value).replace(/\/$/, "");
  if (!path) return;
  const name = path.split("/").filter(Boolean).pop() ?? path;
  try {
    const ws = await wsStore.create(name, path);
    wsStore.open(ws);
    close();
  } catch (e) {
    browseError.value = String(e);
  }
}

const agentsStore = useAgentsStore();
const scriptsStore = useScriptsStore();
const wsStore = useWorkspaceStore();

const ICON_MAP: Record<string, Component> = {
  sparkle: PhSparkle,
  code: PhCode,
  "git-branch": PhGitBranch,
  robot: PhRobot,
  terminal: PhTerminal,
};

function hexBg(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgb(${Math.round(r * 0.11)},${Math.round(g * 0.11)},${Math.round(b * 0.11)})`;
}

interface SpotlightItem {
  id: string;
  title: string;
  desc?: string;
  icon: Component;
  iconColor: string;
  iconBg: string;
  iconBorder: string;
  shortcut?: string;
  dim: boolean;
  action: () => void;
}

const sections = computed(() => {
  if (browsing.value) {
    const items: SpotlightItem[] = [
      { name: "..", up: true },
      ...browseEntries.value.filter((n) => !browseFilter.value || n.toLowerCase().includes(browseFilter.value)).map((n) => ({ name: n, up: false })),
    ].map(({ name, up }) => ({
      id: `dir-${name}`,
      title: name,
      icon: (up ? PhArrowUUpLeft : PhFolder) as Component,
      iconColor: "#a78bfa",
      iconBg: hexBg("#a78bfa"),
      iconBorder: "#a78bfa33",
      dim: true,
      action: () => descend(name),
    }));
    return [{ key: "dirs", label: "DIRECTORIES", items }];
  }

  const q = query.value.toLowerCase().trim();

  const agentItems: SpotlightItem[] = agentsStore.agents
    .filter((a) => !q || a.name.toLowerCase().includes(q) || agentsStore.commandLine(a).includes(q))
    .map((a, i) => ({
      id: `agent-${a.id}`,
      title: `Run ${a.name}`,
      desc: agentsStore.commandLine(a),
      icon: ICON_MAP[a.icon] ?? PhRobot,
      iconColor: a.color,
      iconBg: hexBg(a.color),
      iconBorder: `${a.color}33`,
      shortcut: a.shortcut || undefined,
      dim: i !== 0,
      action: () => { emit("launch", agentsStore.commandLine(a)); close(); },
    }));

  const scriptItems: SpotlightItem[] = scriptsStore.scriptsFor(wsStore.active?.path)
    .filter((s) => scriptsStore.commandLine(s) && (!q || s.name.toLowerCase().includes(q) || scriptsStore.commandLine(s).toLowerCase().includes(q)))
    .map((s) => {
      const color = s.color || "#34d399";
      return {
        id: `script-${s.id}`,
        title: `Run ${s.name}`,
        desc: scriptsStore.commandLine(s),
        icon: PhPlayCircle as Component,
        iconColor: color,
        iconBg: hexBg(color),
        iconBorder: `${color}33`,
        shortcut: undefined,
        dim: true,
        action: () => { emit("launch", scriptsStore.commandLine(s)); close(); },
      };
    });

  const recentWorkspaces = [...wsStore.workspaces]
    .sort((a, b) => (b.last_opened ?? 0) - (a.last_opened ?? 0))
    .slice(0, 3)
    .filter((w) => !q || w.name.toLowerCase().includes(q) || w.path.toLowerCase().includes(q));

  const recentItems: SpotlightItem[] = [
    ...recentWorkspaces.map((w) => ({
      id: `ws-${w.id}`,
      title: w.name,
      desc: w.path,
      icon: PhFolderOpen as Component,
      iconColor: "#a78bfa",
      iconBg: hexBg("#a78bfa"),
      iconBorder: "#a78bfa33",
      shortcut: undefined,
      dim: true,
      action: () => { wsStore.open(w); close(); },
    })),
    ...([
      { id: "cmd-manager", title: "Toggle Manager", icon: PhSparkle as Component, color: "#ec4899", shortcut: "⌘J", action: () => { emit("toggleManager"); close(); } },
      { id: "cmd-settings", title: "Settings → Agents", icon: PhGear as Component, color: "#555555", shortcut: undefined, action: () => { emit("openSettings"); close(); } },
      { id: "cmd-newterm", title: "New Terminal", icon: PhTerminal as Component, color: "#34d399", shortcut: "⌃`", action: () => { emit("newTerminal"); close(); } },
      { id: "cmd-browser", title: "Open Browser Tab", icon: PhGlobe as Component, color: "#60a5fa", shortcut: undefined, action: () => { emit("openBrowser"); close(); } },
      { id: "cmd-repaint", title: "Repaint Terminal (un-scramble)", icon: PhTerminal as Component, color: "#fbbf24", shortcut: "⌘⇧R", action: () => { emit("repaint"); close(); } },
    ] as const)
      .filter(({ title }) => !q || title.toLowerCase().includes(q))
      .map((c) => ({
        id: c.id,
        title: c.title,
        icon: c.icon,
        iconColor: c.color,
        iconBg: hexBg(c.color),
        iconBorder: `${c.color}33`,
        shortcut: c.shortcut,
        dim: true,
        action: c.action,
      })),
  ];

  const cmdDefs: { id: string; title: string; icon: Component; shortcut?: string; action: () => void }[] = [
    { id: "cmd-new-ws", title: "New Workspace", icon: PhPlus as Component, action: () => { emit("newWorkspace"); close(); } },
    { id: "cmd-add-project", title: "Add Project…", icon: PhFolderOpen as Component, action: enterBrowse },
    { id: "cmd-project-config", title: "Project Settings…", icon: PhGear as Component, action: () => { emit("openProjectConfig"); close(); } },
    { id: "cmd-split", title: "Split Terminal", icon: PhColumns as Component, shortcut: "⌘\\", action: () => close() },
    { id: "cmd-theme", title: "Change Theme", icon: PhPalette as Component, action: () => close() },
    { id: "cmd-keys", title: "Keyboard Shortcuts", icon: PhKeyboard as Component, shortcut: "⌘K ⌘S", action: () => close() },
  ];

  const commandItems: SpotlightItem[] = cmdDefs
    .filter((c) => !q || c.title.toLowerCase().includes(q))
    .map((c) => ({
      id: c.id,
      title: c.title,
      icon: c.icon,
      iconColor: "#555555",
      iconBg: "#161616",
      iconBorder: "#55555533",
      shortcut: c.shortcut,
      dim: true,
      action: c.action,
    }));

  const fileItems: SpotlightItem[] = hits.value.map((h, i) => ({
    id: `hit-${i}`,
    title: h.line ? `${h.path}:${h.line}` : h.path,
    desc: h.text || undefined,
    icon: (h.line ? PhMagnifyingGlass : PhFileText) as Component,
    iconColor: "#60a5fa",
    iconBg: hexBg("#60a5fa"),
    iconBorder: "#60a5fa33",
    shortcut: undefined,
    dim: true,
    action: () => openHit(h),
  }));

  return [
    { key: "agents", label: "AGENTS", items: agentItems },
    { key: "scripts", label: "SCRIPTS", items: scriptItems },
    { key: "recent", label: "RECENT", items: recentItems },
    { key: "commands", label: "COMMANDS", items: commandItems },
    { key: "files", label: "IN FILES", items: fileItems },
  ].filter((s) => s.items.length > 0);
});

const flatItems = computed(() => sections.value.flatMap((s) => s.items));

function selectFirst() {
  selectedId.value = flatItems.value[0]?.id ?? "";
}

watch(query, () => nextTick(selectFirst));

function move(dir: 1 | -1) {
  const items = flatItems.value;
  const idx = items.findIndex((i) => i.id === selectedId.value);
  const next = Math.max(0, Math.min(items.length - 1, idx + dir));
  selectedId.value = items[next]?.id ?? "";
}

function activate() {
  flatItems.value.find((i) => i.id === selectedId.value)?.action();
}

function runItem(item: SpotlightItem) {
  item.action();
}

function show() {
  isOpen.value = true;
  browsing.value = false;
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

defineExpose({ show, close });
</script>

<style scoped>
/* Custom scrollbar (::-webkit-scrollbar has no Tailwind utility) and the
   Vue <Transition> phase classes stay as real CSS. */
.s-results::-webkit-scrollbar { width: 4px; }
.s-results::-webkit-scrollbar-track { background: transparent; }
.s-results::-webkit-scrollbar-thumb { background: #2a2a2a; border-radius: 2px; }

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
