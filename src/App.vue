<template>
  <div class="ide-root">
    <TitleBar
      :workspace-name="ws.active?.name"
      :branch="ws.active?.is_git === false ? '' : git.branch"
      :folder-path="ws.active?.path"
      :right-panel-visible="ui.rightPanelVisible"
      @back="ws.close()"
      @toggle-rightpanel="ui.toggleRightPanel()"
      @open-settings="ui.openSettings()"
    />
    <Settings v-if="ui.settingsOpen" @close="ui.closeSettings()" />
    <div class="ide-body" :class="{ 'panels-swapped': ui.swapPanels }" :style="panelStyles">
      <ActivityBar class="panel-activity" />
      <Sidebar class="panel-sidebar" />
      <div class="resize-handle panel-resize-left" @mousedown="startResize('left', $event)" />
      <div class="ide-main">
        <!-- Terminals stay MOUNTED across every mode (v-show, not v-if). Unmounting
             them on a mode switch ran XTerm.onBeforeUnmount → detach_pty + dispose;
             returning replayed the daemon ring-buffer into a fresh fit → scrambled
             buffer. Keeping them mounted avoids the detach/reattach entirely. -->
        <div v-show="ui.mode === 'terminal'" class="terminal-host">
          <div v-show="!ws.active" class="no-workspace">
            <PhFolderOpen :size="32" weight="thin" />
            <span>Select a workspace</span>
          </div>
          <Terminal
            v-for="w in ws.opened"
            v-show="ws.active && w.id === ws.active.id"
            :key="w.id"
            :workspace-id="w.id"
            :cwd="w.path"
            :ref="(el) => setTermRef(w.id, el)"
          />
        </div>
        <Dashboard v-if="ui.mode === 'dashboard'" class="dashboard-main-panel" @new-workspace="openNewWorkspace" />
        <GitPanel v-else-if="ui.mode === 'git'" class="git-main-panel" />
        <MissionControl v-else-if="ui.mode === 'mission'" class="mission-main-panel" />
      </div>
      <div v-show="ui.rightPanelVisible" class="resize-handle panel-resize-right" @mousedown="startResize('right', $event)" />
      <RightPanel v-show="ui.rightPanelVisible" class="panel-right" :cwd="ws.active?.path ?? ''" :is-git="ws.active?.is_git !== false" />
    </div>
    <ManagerBar
      v-if="ui.floatChatEnabled && ws.active"
      :cwd="ws.active.path"
      :ws-id="ws.active.id"
      @open-project-config="showProjectConfig = true"
    />
    <WorkspaceConfig
      v-if="showProjectConfig && ws.active"
      :workspace-path="ws.active.path"
      :workspace-name="ws.active.name"
      @close="showProjectConfig = false"
    />
    <Spotlight
      ref="spotlightRef"
      @launch="(cmd) => activeTerm()?.spawnAgent(cmd)"
      @new-terminal="activeTerm()?.addTab()"
      @new-workspace="openNewWorkspace"
      @open-settings="ui.openSettings()"
      @open-browser="activeTerm()?.openBrowserTab()"
      @repaint="activeTerm()?.repaintAll()"
      @toggle-manager="ui.toggleFloatChat()"
    />
    <ToastStack />
    <UpdateBanner />
    <DiagramModal v-if="diagramContent !== null" />
    <Teleport to="body"><PetOverlay v-if="ui.petsEnabled" /></Teleport>

    <!-- Keyboard cheatsheet overlay (⌘/) -->
    <Teleport to="body">
      <div v-if="cheatsheetOpen" class="cheatsheet-backdrop" @click.self="cheatsheetOpen = false">
        <div class="cheatsheet-panel">
          <div class="cheatsheet-header">
            <span class="cheatsheet-title">Keyboard Shortcuts</span>
            <button class="cheatsheet-close" @click="cheatsheetOpen = false"><PhX :size="14" /></button>
          </div>
          <div class="cheatsheet-body">
            <div v-for="group in CHEATSHEET_GROUPS" :key="group.label" class="cs-group">
              <div class="cs-group-label">{{ group.label }}</div>
              <div v-for="s in group.shortcuts" :key="s.keys" class="cs-row">
                <span class="cs-desc">{{ s.desc }}</span>
                <span class="cs-keys">
                  <kbd v-for="k in s.keys.split(' ')" :key="k" class="cs-key">{{ k }}</kbd>
                </span>
              </div>
            </div>
          </div>
          <div class="cheatsheet-hint"><kbd class="cs-key">Esc</kbd> to close</div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed, provide, watch, nextTick } from "vue";
import { PhFolderOpen, PhX } from "@phosphor-icons/vue";
import TitleBar from "@/components/TitleBar.vue";
import Sidebar from "@/components/Sidebar.vue";
import ActivityBar from "@/components/ActivityBar.vue";
import Terminal from "@/components/Terminal.vue";
import RightPanel from "@/components/RightPanel.vue";
import GitPanel from "@/components/GitPanel.vue";
import MissionControl from "@/components/MissionControl.vue";
import Dashboard from "@/components/Dashboard.vue";
import Settings from "@/components/Settings.vue";
import Spotlight from "@/components/Spotlight.vue";
import ToastStack from "@/components/ToastStack.vue";
import UpdateBanner from "@/components/UpdateBanner.vue";
import PetOverlay from "@/components/PetOverlay.vue";
import ManagerBar from "@/components/ManagerBar.vue";
import WorkspaceConfig from "@/components/WorkspaceConfig.vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useGitStore } from "@/stores/git";
import { useAgentsStore } from "@/stores/agents";
import { useUpdateStore } from "@/stores/update";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { matchesShortcut } from "@/lib/shortcuts";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import DiagramModal from "@/components/DiagramModal.vue";
import { useDiagram } from "@/composables/useDiagram";

let resizing: 'left' | 'right' | null = null;
let resizeStartX = 0;
let resizeStartWidth = 0;

const showProjectConfig = ref(false);

const ws = useWorkspaceStore();
const ui = useUIStore();
const git = useGitStore();
const { diagramContent } = useDiagram();
const agents = useAgentsStore();
const update = useUpdateStore();
const tabsStore = useTerminalTabsStore();

const panelStyles = computed(() => ({
  '--sidebar-width': ui.sidebarWidth + 'px',
  '--right-panel-width': ui.rightPanelWidth + 'px',
}));

function startResize(side: 'left' | 'right', e: MouseEvent) {
  resizing = side;
  resizeStartX = e.clientX;
  resizeStartWidth = side === 'left' ? ui.sidebarWidth : ui.rightPanelWidth;
  e.preventDefault();
}

function onResizeMove(e: MouseEvent) {
  if (!resizing) return;
  const delta = e.clientX - resizeStartX;
  if (!ui.swapPanels) {
    if (resizing === 'left') {
      ui.sidebarWidth = Math.min(400, Math.max(150, resizeStartWidth + delta));
    } else {
      ui.rightPanelWidth = Math.min(500, Math.max(200, resizeStartWidth - delta));
    }
  } else {
    if (resizing === 'right') {
      ui.rightPanelWidth = Math.min(500, Math.max(200, resizeStartWidth + delta));
    } else {
      ui.sidebarWidth = Math.min(400, Math.max(150, resizeStartWidth - delta));
    }
  }
}

function onResizeUp() {
  resizing = null;
}

// Sync OS window title: "<tab title> — <workspace>" so macOS Mission Control /
// Alt+Tab shows meaningful context. Lazy Tauri import so browser dev doesn't crash.
watch(
  () => {
    const wsId = ws.active?.id;
    if (!wsId) return ws.active?.name ?? "Burrow";
    const activeTabId = tabsStore.activeByWs[wsId];
    const tab = tabsStore.tabsByWs[wsId]?.find((t) => t.id === activeTabId);
    const tabTitle = tab?.title ?? "";
    const wsName = ws.active?.name ?? "";
    return tabTitle && tabTitle !== wsName ? `${tabTitle} — ${wsName}` : (wsName || "Burrow");
  },
  (title) => {
    import("@tauri-apps/api/window")
      .then(({ getCurrentWindow }) => getCurrentWindow().setTitle(title))
      .catch(() => {});
  },
  { immediate: true },
);

// Terminals are display:none while another mode is active; a ResizeObserver tick
// can settle a 0-size fit. Refit the active terminal when we return to it so the
// PTY matches the real pane geometry.
watch(
  () => ui.mode,
  (m) => {
    if (m === "terminal") nextTick(() => activeTerm()?.refitAll());
  },
);

// When the active workspace changes, repaint its terminals — they may have been
// v-show:false while inactive and missed a resize, leaving stale glyph renders.
watch(
  () => ws.active?.id,
  () => { nextTick(() => activeTerm()?.repaintAll()); },
);

// Prevent Mac sleep while any agent tab is busy
watch(
  () => {
    const allTabs = Object.values(tabsStore.tabsByWs).flat();
    return allTabs.some((t) => t.busy);
  },
  (anyBusy) => {
    import("@tauri-apps/api/core")
      .then(({ invoke }) => invoke("set_sleep_inhibit", { active: anyBusy }))
      .catch(() => {});
  },
);

const spotlightRef = ref<InstanceType<typeof Spotlight> | null>(null);
const cheatsheetOpen = ref(false);

const CHEATSHEET_GROUPS = [
  {
    label: "Global",
    shortcuts: [
      { keys: "⌘ ,",   desc: "Settings" },
      { keys: "⌘ P",   desc: "Command Palette" },
      { keys: "⌘ J",   desc: "Toggle Manager" },
      { keys: "⌘ /",   desc: "Keyboard Shortcuts" },
      { keys: "⌘ ⇧ U", desc: "Jump to unread tab" },
      { keys: "Esc",   desc: "Close overlay" },
    ],
  },
  {
    label: "Tabs & Panes",
    shortcuts: [
      { keys: "⌘ T",   desc: "New tab" },
      { keys: "⌘ W",   desc: "Close pane" },
      { keys: "⌘ D",   desc: "Split horizontal" },
      { keys: "⌘ ⇧ D", desc: "Split vertical" },
    ],
  },
  {
    label: "Terminal",
    shortcuts: [
      { keys: "⇧ ↵",  desc: "Multiline input (Claude)" },
    ],
  },
  {
    label: "Projects",
    shortcuts: [
      { keys: "⌘ 1-9", desc: "Switch project" },
    ],
  },
  {
    label: "Agents",
    shortcuts: [
      { keys: "⌘ ⇧ 1-5", desc: "Launch agent (configurable)" },
    ],
  },
];

// One Terminal stays mounted per opened workspace; resolve the active one for
// commands (Spotlight launch, new terminal).
const termRefs = new Map<number, InstanceType<typeof Terminal>>();
function setTermRef(id: number, el: unknown) {
  if (el) termRefs.set(id, el as InstanceType<typeof Terminal>);
  else termRefs.delete(id);
}
function activeTerm() {
  return ws.active ? termRefs.get(ws.active.id) : undefined;
}

provide('activeTerm', activeTerm);


async function openNewWorkspace() {
  const dir = await openDialog({ directory: true, multiple: false });
  if (!dir || typeof dir !== "string") return;
  const name = dir.split("/").filter(Boolean).pop() ?? dir;
  await ws.create(name, dir);
}

// Check for updates at startup (after a short delay so it doesn't compete with
// the initial PTY/workspace load) and every 6 hours after. Silent: failures in
// browser-only dev (no Tauri) are swallowed.
let updateTimer: number | undefined;
let unlistenFloat: UnlistenFn | null = null;
let unlistenMenuUpdate: UnlistenFn | null = null;
let unlistenWorkspacesChanged: UnlistenFn | null = null;

// Short warm synth arpeggio on app startup — no asset, no user gesture needed in
// the app webview; fails silently if the AudioContext is blocked.
function playStartupChime() {
  try {
    const AC = window.AudioContext || (window as any).webkitAudioContext;
    if (!AC) return;
    const ctx = new AC();
    const now = ctx.currentTime;
    const master = ctx.createGain();
    master.gain.setValueAtTime(0.0001, now);
    master.gain.exponentialRampToValueAtTime(0.5, now + 0.04);
    master.gain.exponentialRampToValueAtTime(0.0001, now + 1.6);
    master.connect(ctx.destination);
    const notes = [329.63, 415.3, 493.88, 659.25]; // E4 G#4 B4 E5
    notes.forEach((f, i) => {
      const t = now + i * 0.085;
      const osc = ctx.createOscillator();
      const g = ctx.createGain();
      osc.type = "triangle";
      osc.frequency.value = f;
      g.gain.setValueAtTime(0.0001, t);
      g.gain.exponentialRampToValueAtTime(0.32, t + 0.03);
      g.gain.exponentialRampToValueAtTime(0.0001, t + 0.9);
      osc.connect(g).connect(master);
      osc.start(t);
      osc.stop(t + 1.0);
    });
    ctx.resume();
  } catch {
    /* silent */
  }
}

onMounted(async () => {
  ws.load();
  playStartupChime();
  window.addEventListener("keydown", onKeydown);
  window.addEventListener('mousemove', onResizeMove);
  window.addEventListener('mouseup', onResizeUp);
  setTimeout(() => update.check({ silent: true }), 3000);
  updateTimer = window.setInterval(() => update.check({ silent: true }), 6 * 60 * 60 * 1000);

  unlistenMenuUpdate = await listen("menu-check-update", () => {
    update.check({ silent: false });
  });

  unlistenWorkspacesChanged = await listen("workspaces-changed", () => {
    ws.load();
  });

  // Float bubble "focus main" — switch to the right workspace + leaf
  unlistenFloat = await listen<{ ptyId: number; wsId: number }>(
    "float-focus-tab",
    ({ payload }) => {
      const target = ws.workspaces.find((w) => w.id === payload.wsId);
      if (target) ws.open(target);
      // Defer focus until Terminal for that workspace is mounted/active
      setTimeout(() => {
        const term = termRefs.get(payload.wsId);
        term?.focusLeaf(payload.ptyId);
      }, 50);
    },
  );
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener('mousemove', onResizeMove);
  window.removeEventListener('mouseup', onResizeUp);
  if (updateTimer) clearInterval(updateTimer);
  unlistenMenuUpdate?.();
  unlistenFloat?.();
  unlistenWorkspacesChanged?.();
});

function onKeydown(e: KeyboardEvent) {
  // Don't let agent-launch shortcuts fire while the user is typing in a text
  // field (chat textarea, rename input, …) unless a ⌘/⌃ modifier is held — a
  // bare/Shift-only agent shortcut would otherwise match a normal keystroke and
  // yank focus to a freshly spawned tab. ponytail: matches on element type, not
  // a focus registry; add contenteditable check if a rich editor lands.
  const t = e.target as HTMLElement | null;
  const typing =
    !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
  // Agent launch shortcuts (user-configured per agent). Checked first so a
  // bound combo wins; defaults use ⌘⇧1-5 to avoid the plain ⌘1-9 ws switch.
  if (!typing || e.metaKey || e.ctrlKey) {
    for (const a of agents.agents) {
      if (a.command.trim() && matchesShortcut(e, a.shortcut)) {
        e.preventDefault();
        activeTerm()?.spawnAgent(agents.commandLine(a));
        return;
      }
    }
  }
  // ⌘⇧R — repaint terminals (un-scramble) even when xterm isn't focused.
  // preventDefault to stop the webview hard-reloading on this combo.
  if (e.metaKey && e.shiftKey && !e.ctrlKey && !e.altKey && (e.key === "R" || e.key === "r")) {
    e.preventDefault();
    activeTerm()?.repaintAll();
    return;
  }
  // ⌘⇧U — jump to first unread (review) tab across ALL workspaces
  if (e.metaKey && e.shiftKey && !e.ctrlKey && !e.altKey && e.key === "U") {
    e.preventDefault();
    for (const [wsId, wsTabs] of Object.entries(tabsStore.tabsByWs)) {
      const reviewTab = wsTabs.find((t) => t.status === "review");
      if (reviewTab) {
        const targetWs = ws.workspaces.find((w) => w.id === Number(wsId));
        if (targetWs) ws.open(targetWs);
        tabsStore.activate(Number(wsId), reviewTab.id);
        break;
      }
    }
    return;
  }
  if (e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey) {
    if (e.key === ",") {
      e.preventDefault();
      ui.toggleSettings();
    } else if (e.key === "p") {
      e.preventDefault();
      spotlightRef.value?.show();
    } else if (e.key === "/") {
      e.preventDefault();
      cheatsheetOpen.value = !cheatsheetOpen.value;
    } else if (e.key === "j") {
      e.preventDefault();
      ui.toggleFloatChat();
    } else if (/^[1-9]$/.test(e.key)) {
      e.preventDefault();
      const idx = parseInt(e.key) - 1;
      const target = ws.workspaces[idx];
      if (target) ws.open(target);
    }
  } else if (e.key === "Escape") {
    if (cheatsheetOpen.value) {
      e.preventDefault();
      cheatsheetOpen.value = false;
    } else if (ui.settingsOpen) {
      e.preventDefault();
      ui.closeSettings();
    }
  }
}
</script>

<style>
@import "@/styles/status-dots.css";

* {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

:root {
  --bg-base: #0d0d0d;
  --terminal-bg: #0a0a0a;
  --bg-panel: #111111;
  --bg-dropdown: #111111;
  --bg-hover: #1a1a1a;
  --bg-selected: #1e3a5f;
  --border: #2a2a2a;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent: #3b82f6;
  --accent-dim: #1d4ed8;
  --green: #22c55e;
  --yellow: #eab308;
  --red: #ef4444;
  --font-mono: "JetBrains Mono", "Fira Code", "Cascadia Code", monospace;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  --sidebar-width: 220px;
  --right-panel-width: 300px;
  --titlebar-height: 36px;
}

body {
  background-color: var(--bg-base);
  /* Meme themes set --bg-image to a tiled wallpaper; normal themes = none. */
  background-image: var(--bg-image, none);
  background-attachment: fixed;
  color: var(--text-primary);
  font-family: var(--font-ui);
  overflow: hidden;
  user-select: none;
  /* macOS renders text heavy/soft on dark bg without this — antialiased makes it crisp. */
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
}

/* #app fills the window; UI scale is applied via `zoom` in the ui store. */
#app {
  width: 100vw;
  height: 100vh;
}

.ide-root {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
}

.ide-body {
  display: flex;
  flex: 1;
  overflow: hidden;
}

.ide-main {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.terminal-host {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.git-main-panel {
  flex: 1;
  overflow: hidden;
}

.mission-main-panel {
  flex: 1;
  overflow: hidden;
}

.dashboard-main-panel {
  flex: 1;
  overflow: hidden;
}

.resize-handle {
  width: 4px;
  cursor: col-resize;
  flex-shrink: 0;
  background: transparent;
  transition: background 0.15s;
  position: relative;
  z-index: 10;
}
.resize-handle:hover,
.resize-handle:active {
  background: var(--accent);
  opacity: 0.4;
}

.panels-swapped .panel-activity { order: 0; }
.panels-swapped .panel-sidebar { order: 5; }
.panels-swapped .panel-resize-left { order: 4; }
.panels-swapped .ide-main { order: 3; }
.panels-swapped .panel-resize-right { order: 2; }
.panels-swapped .panel-right { order: 1; }

.no-workspace {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--text-secondary);
  font-size: 13px;
}

/* Cheatsheet overlay */
.cheatsheet-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
}
.cheatsheet-panel {
  background: #0f0f0f;
  border: 1px solid #222;
  border-radius: 10px;
  width: 480px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.7);
}
.cheatsheet-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 12px;
  border-bottom: 1px solid #1a1a1a;
}
.cheatsheet-title {
  font-size: 13px;
  font-weight: 600;
  color: #e2e8f0;
  letter-spacing: 0.01em;
}
.cheatsheet-close {
  background: none;
  border: none;
  color: #555;
  cursor: pointer;
  padding: 2px;
  display: flex;
  align-items: center;
}
.cheatsheet-close:hover { color: #888; }
.cheatsheet-body {
  overflow-y: auto;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.cs-group-label {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #555;
  margin-bottom: 6px;
}
.cs-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 0;
  gap: 12px;
}
.cs-desc { font-size: 12px; color: #94a3b8; }
.cs-keys { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
.cs-key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  border-radius: 4px;
  background: #1a1a1a;
  border: 1px solid #2a2a2a;
  color: #cbd5e1;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  line-height: 1.4;
}
.cheatsheet-hint {
  padding: 10px 16px;
  border-top: 1px solid #1a1a1a;
  font-size: 11px;
  color: #444;
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
