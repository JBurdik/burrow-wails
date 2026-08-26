<template>
  <Teleport to="body">
    <Transition name="spotlight">
      <div v-if="isOpen" class="fixed inset-0 z-[9000] flex justify-center bg-black/60 pt-[165px]" @mousedown.self="close">
        <div class="s-modal flex max-h-[600px] w-[680px] flex-col self-start overflow-hidden rounded-xl border border-[#2a2a2a] bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.6),0_1px_0_rgba(255,255,255,0.08)] [backdrop-filter:var(--blur-overlay,none)]">
          <div class="flex h-14 shrink-0 items-center gap-3 border-b border-[#1e1e1e] px-4">
            <PhTerminal :size="18" color="#ec4899" />
            <input
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
            <div class="shrink-0 rounded border border-[#2a2a2a] bg-[#1a1a1a] px-2 py-0.5 text-[11px] text-[#555]">esc</div>
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

          <div class="flex h-9 shrink-0 items-center gap-4 border-t border-[#1e1e1e] bg-[#0d0d0d] px-4">
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
} from "@phosphor-icons/vue";
import { useAgentsStore } from "@/stores/agents";
import { useScriptsStore } from "@/stores/scripts";
import { useWorkspaceStore } from "@/stores/workspace";
import type { Component } from "vue";

const emit = defineEmits<{
  launch: [cmd: string];
  newTerminal: [];
  newWorkspace: [];
  openSettings: [];
  openBrowser: [];
  repaint: [];
  toggleManager: [];
}>();

const isOpen = ref(false);
const query = ref("");
const selectedId = ref("");
const inputRef = ref<HTMLInputElement | null>(null);

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

  return [
    { key: "agents", label: "AGENTS", items: agentItems },
    { key: "scripts", label: "SCRIPTS", items: scriptItems },
    { key: "recent", label: "RECENT", items: recentItems },
    { key: "commands", label: "COMMANDS", items: commandItems },
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
  query.value = "";
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
