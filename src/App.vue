<template>
  <div class="ide-root">
    <TitleBar
      :workspace-name="ws.active?.name"
      :branch="git.cwd === ws.active?.path ? git.branch : ''"
      :folder-path="ws.active?.path"
      :right-panel-visible="ui.rightPanelVisible"
      :sidebar-visible="ui.sidebarVisible"
      @toggle-sidebar="ui.toggleSidebar()"
      @back="ws.close()"
      @toggle-rightpanel="ui.toggleRightPanel()"
    />
    <Settings v-if="ui.settingsOpen" @close="ui.closeSettings()" />
    <div class="ide-body" :class="{ 'panels-swapped': ui.swapPanels }" :style="panelStyles">
      <Sidebar v-show="ui.sidebarVisible" class="panel-sidebar" />
      <div v-show="ui.sidebarVisible" class="resize-handle panel-resize-left" @mousedown="startResize('left', $event)" />
      <div class="ide-main">
        <!-- Terminals stay MOUNTED across every mode (v-show, not v-if). Unmounting
             them on a mode switch ran XTerm.onBeforeUnmount → detach_pty + dispose;
             returning replayed the daemon ring-buffer into a fresh fit → scrambled
             buffer. Keeping them mounted avoids the detach/reattach entirely. -->
        <div v-show="ui.mode === 'terminal'" class="terminal-host">
          <WelcomeScreen ref="welcomeEl" v-show="showWelcome" @open-folder="openNewWorkspace" />
          <Terminal
            v-for="w in ws.opened"
            v-show="ws.active && w.id === ws.active.id && !showWelcome"
            :key="w.id"
            :workspace-id="w.id"
            :cwd="w.path"
            :ref="(el) => setTermRef(w.id, el)"
          />
        </div>
        <Dashboard v-if="ui.mode === 'dashboard'" class="dashboard-main-panel" @new-workspace="openNewWorkspace" />
      </div>
      <div v-show="ui.rightPanelVisible" class="resize-handle panel-resize-right" @mousedown="startResize('right', $event)" />
      <RightPanel
        ref="rightPanelRef"
        class="panel-right"
        :open="ui.rightPanelVisible"
        :cwd="ws.active?.path ?? ''"
        :workspace-id="ws.active?.id"
        :is-git="ws.active?.is_git !== false"
        @open-panel="ui.rightPanelVisible = true"
        @close-panel="ui.rightPanelVisible = false"
        @manager-open="ui.rightPanelWidth = Math.max(ui.rightPanelWidth, 440)"
        @open-project-config="showProjectConfig = true"
      />
    </div>
    <WorkspaceConfig
      v-if="showProjectConfig && ws.active"
      :workspace-id="ws.active.parent_id ?? ws.active.id"
      @close="showProjectConfig = false"
    />
    <Spotlight
      ref="spotlightRef"
      @launch="(cmd) => activeTerm()?.spawnAgent(cmd)"
      @new-terminal="activeTerm()?.addTab()"
      @open-project-config="showProjectConfig = true"
      @open-browser="activeTerm()?.openBrowserTab()"
      @repaint="activeTerm()?.repaintAll()"
      @split-terminal="activeTerm()?.splitPane('h')"
      @toggle-manager="openManagerPanel"
      @open-file="openSearchHit"
    />
    <PathPicker />
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
            <div v-for="group in cheatGroups" :key="group.label" class="cs-group">
              <div class="cs-group-label">{{ group.label }}</div>
              <div v-for="s in group.shortcuts" :key="s.desc" class="cs-row">
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
import { ref, onMounted, onBeforeUnmount, computed, provide, watch, nextTick, useTemplateRef } from "vue";
import { PhX } from "@phosphor-icons/vue";
import TitleBar from "@/components/TitleBar.vue";
import Sidebar from "@/components/Sidebar.vue";
import Terminal from "@/components/Terminal.vue";
import RightPanel from "@/components/RightPanel.vue";
import Dashboard from "@/components/Dashboard.vue";
import Settings from "@/components/Settings.vue";
import Spotlight from "@/components/Spotlight.vue";
import ToastStack from "@/components/ToastStack.vue";
import UpdateBanner from "@/components/UpdateBanner.vue";
import PetOverlay from "@/components/PetOverlay.vue";
import WorkspaceConfig from "@/components/WorkspaceConfig.vue";
import WelcomeScreen from "@/components/WelcomeScreen.vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useGitStore } from "@/stores/git";
import { useProvidersStore } from "@/stores/providers";
import { useUpdateStore } from "@/stores/update";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { matchesShortcut } from "@/lib/shortcuts";
import { useKeybindingsStore } from "@/stores/keybindings";
import { FIXED_SHORTCUTS } from "@/lib/keymap";
import PathPicker from "@/components/PathPicker.vue";
import { pickDir } from "@/lib/pickPath";
import { installControlBridge } from "@/lib/controlBridge";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import DiagramModal from "@/components/DiagramModal.vue";
import { useDiagram } from "@/composables/useDiagram";

let resizing: 'left' | 'right' | null = null;
let resizeStartX = 0;
let resizeStartWidth = 0;

const showProjectConfig = ref(false);
const rightPanelRef = useTemplateRef<InstanceType<typeof RightPanel>>("rightPanelRef");

function openManagerPanel() {
  ui.rightPanelVisible = true;
  ui.rightPanelWidth = Math.max(ui.rightPanelWidth, 440);
  nextTick(() => rightPanelRef.value?.openManager());
}

const ws = useWorkspaceStore();
const ui = useUIStore();
const git = useGitStore();
const { diagramContent } = useDiagram();
const providers = useProvidersStore();
const keys = useKeybindingsStore();
const update = useUpdateStore();
const tabsStore = useTerminalTabsStore();

// No active workspace, or the active one has no tabs open yet.
const showWelcome = computed(() =>
  ui.welcomeOpen || !ws.active || (tabsStore.tabsByWs[ws.active.id]?.length ?? 0) === 0,
);

// Screen stays mounted behind v-show, so re-focus its composer each time it shows.
const welcomeEl = useTemplateRef<{ focus: () => void; cycleProvider: () => void }>("welcomeEl");
watch(showWelcome, (on) => { if (on) nextTick(() => welcomeEl.value?.focus()); });

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
      ui.sidebarWidth = Math.min(window.innerWidth * 0.5, Math.max(150, resizeStartWidth + delta));
    } else {
      ui.rightPanelWidth = Math.min(window.innerWidth * 0.5, Math.max(200, resizeStartWidth - delta));
    }
  } else {
    if (resizing === 'right') {
      ui.rightPanelWidth = Math.min(window.innerWidth * 0.5, Math.max(200, resizeStartWidth + delta));
    } else {
      ui.sidebarWidth = Math.min(window.innerWidth * 0.5, Math.max(150, resizeStartWidth - delta));
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
watch(spotlightRef, (v) => { if (v) ui.registerSpotlightApi({ show: (opts) => v.show(opts) }); });
const cheatsheetOpen = ref(false);

// Cheatsheet renders the live keybindings (so a rebind shows here too) plus the
// handful of fixed range shortcuts that can't be expressed as one combo.
const cheatGroups = computed(() => {
  const groups = keys.groups.map((g) => ({
    label: g.label,
    shortcuts: g.commands.filter((c) => c.keys).map((c) => ({ keys: c.keys.split("").join(" "), desc: c.label })),
  }));
  for (const f of FIXED_SHORTCUTS) {
    const g = groups.find((x) => x.label === f.group);
    if (g) g.shortcuts.push({ keys: f.keys, desc: f.desc });
    else groups.push({ label: f.group, shortcuts: [{ keys: f.keys, desc: f.desc }] });
  }
  return groups.filter((g) => g.shortcuts.length);
});

// One Terminal stays mounted per opened workspace; resolve the active one for
// commands (Spotlight launch, new terminal).
const termRefs = new Map<number, InstanceType<typeof Terminal>>();
function setTermRef(id: number, el: unknown) {
  if (el) termRefs.set(id, el as InstanceType<typeof Terminal>);
  else termRefs.delete(id);
}
// A ⌘P search hit carries a repo-relative path; the editor wants an absolute one.
function openSearchHit(path: string, line: number) {
  const root = ws.active?.path;
  if (!root) return;
  const abs = path.startsWith("/") ? path : `${root.replace(/\/$/, "")}/${path}`;
  ui.setMode("terminal");
  nextTick(() => activeTerm()?.openFileInTab(abs, path.split("/").pop() ?? path, line || undefined));
}

function activeTerm() {
  return ws.active ? termRefs.get(ws.active.id) : undefined;
}

provide('activeTerm', activeTerm);


async function openNewWorkspace() {
  const dir = await pickDir({ title: "Add project", start: "~/" });
  if (!dir) return;
  const name = dir.split("/").filter(Boolean).pop() ?? dir;
  const created = await ws.create(name, dir);
  if (created) ws.open(created);
}

// Check for updates at startup (after a short delay so it doesn't compete with
// the initial PTY/workspace load) and every 6 hours after. Silent: failures in
// browser-only dev (no Tauri) are swallowed.
let updateTimer: number | undefined;
let unlistenMenuUpdate: UnlistenFn | null = null;
let unlistenControl: (() => void) | undefined;
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

  // The control API's UI half: one app-wide listener performing the actions
  // agents (and the Manager) ask for, and acking each with its result.
  unlistenControl = await installControlBridge();
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener('mousemove', onResizeMove);
  window.removeEventListener('mouseup', onResizeUp);
  if (updateTimer) clearInterval(updateTimer);
  unlistenMenuUpdate?.();
  unlistenWorkspacesChanged?.();
  unlistenControl?.();
});

// One handler per rebindable app-scope command; the keybindings store owns the
// combos, so a rebind in Settings (or a hand-edit of config.json) takes effect
// with no code change here.
const KEY_ACTIONS: Record<string, () => void> = {
  palette: () => spotlightRef.value?.show(),
  settings: () => ui.toggleSettings(),
  cheatsheet: () => { cheatsheetOpen.value = !cheatsheetOpen.value; },
  sidebar: () => ui.toggleSidebar(),
  manager: () => openManagerPanel(),
  repaint: () => activeTerm()?.repaintAll(),
  newProject: () => openNewWorkspace(),
  pickProject: () => ui.pickProjectThenWelcome(),
  switchProvider: () => welcomeEl.value?.cycleProvider?.(),
  unread: jumpToUnread,
};

// ⌘⇧U across ALL workspaces (Terminal.vue handles the in-workspace case first).
function jumpToUnread() {
  for (const [wsId, wsTabs] of Object.entries(tabsStore.tabsByWs)) {
    const reviewTab = wsTabs.find((t) => t.status === "review");
    if (reviewTab) {
      const targetWs = ws.workspaces.find((w) => w.id === Number(wsId));
      if (targetWs) ws.open(targetWs);
      tabsStore.activate(Number(wsId), reviewTab.id);
      return;
    }
  }
}

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
    for (const a of providers.active) {
      if (a.terminalShortcut && matchesShortcut(e, a.terminalShortcut)) {
        e.preventDefault();
        activeTerm()?.spawnAgent(providers.commandLine(a));
        return;
      }
    }
  }
  for (const cmd of keys.inScope("app")) {
    if (KEY_ACTIONS[cmd.id] && matchesShortcut(e, cmd.keys)) {
      e.preventDefault();
      KEY_ACTIONS[cmd.id]();
      return;
    }
  }
  // ⌘1-9 project switch — a range, so not part of the rebindable registry.
  if (e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && /^[1-9]$/.test(e.key)) {
    e.preventDefault();
    const target = ws.workspaces[parseInt(e.key) - 1];
    if (target) ws.open(target);
    return;
  }
  if (e.key === "Escape") {
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
@import "tailwindcss";
@import "tw-animate-css";
@import "@/styles/status-dots.css";
@import "@/styles/composer.css";

/* Bridges the runtime theme system (themes.ts sets these on :root via JS —
   switching a Burrow theme must re-skin Tailwind utility classes too, not
   just the old hand-written CSS) into Tailwind's color tokens, so
   `bg-panel`, `text-primary`, `border-border`, `bg-accent` etc. all work as
   Tailwind utilities and stay in sync with the active theme. */
@theme inline {
  --color-base: var(--bg-base);
  --color-terminal-bg: var(--terminal-bg);
  --color-panel: var(--bg-panel);
  --color-hover: var(--bg-hover);
  --color-selected: var(--bg-selected);
  --color-border: var(--border);
  --color-foreground: var(--text-primary);
  --color-secondary-foreground: var(--text-secondary);
  --color-muted-foreground: var(--text-muted);
  --color-accent: var(--accent);
  --color-accent-dim: var(--accent-dim);
  --color-success: var(--green);
  --color-warning: var(--yellow);
  --color-destructive: var(--red);
  --font-sans: var(--font-ui);
  --font-mono: var(--font-mono);

  /* shadcn-vue's default component set expects these token names too. */
  --color-background: var(--bg-base);
  --color-card: var(--bg-panel);
  --color-card-foreground: var(--text-primary);
  --color-popover: var(--bg-dropdown);
  --color-popover-foreground: var(--text-primary);
  --color-primary: var(--accent);
  --color-primary-foreground: var(--bg-base);
  --color-secondary: var(--bg-hover);
  --color-muted: var(--bg-hover);
  --color-input: var(--border);
  --color-ring: var(--accent);
  --radius: 0.375rem;
}

/* Window dragging under Wails. Wails reads the *computed* `--wails-draggable`
   of the mousedown target; custom properties inherit, so the region opts in
   and interactive descendants opt back out. The markup keeps Tauri's
   `data-tauri-drag-region` / `[-webkit-app-region:no-drag]` names from the
   Rust app — these two rules are the whole port. */
[data-tauri-drag-region] {
  --wails-draggable: drag;
}
[data-tauri-drag-region] :where(button, a, input, textarea, select, [role="button"], [class*="no-drag"]) {
  --wails-draggable: no-drag;
}

/* MUST be @layer base, not unlayered: Tailwind v4's utilities live in
   @layer utilities, and unlayered CSS always wins over ANY layered rule
   regardless of specificity — an unlayered `* { padding: 0 }` was silently
   nuking every Tailwind p-*/m-* utility app-wide. */
@layer base {
  * {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
  }
}

:root {
  /* T3 Code-inspired palette: deep purple-black, pink/magenta accent,
     monospace throughout (matches the reference screenshots). */
  --bg-base: #0e0c14;
  --terminal-bg: #0a0810;
  --bg-panel: #16131f;
  --bg-dropdown: #1a1726;
  --bg-hover: #201c2d;
  --bg-selected: #2a2138;
  --border: #262233;
  --text-primary: #f0eef7;
  --text-secondary: #a9a2c0;
  --text-muted: #6f6885;
  --accent: #ec4899;
  --accent-dim: #be185d;
  --green: #34d399;
  --yellow: #fbbf24;
  --red: #f87171;
  --font-mono: "JetBrains Mono", "0xProto Nerd Font", "Fira Code", "Cascadia Code", monospace;
  --font-ui: "JetBrains Mono", "0xProto Nerd Font", "Fira Code", monospace;
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

.panels-swapped .panel-sidebar { order: 5; }
.panels-swapped .panel-resize-left { order: 4; }
.panels-swapped .ide-main { order: 3; }
.panels-swapped .panel-resize-right { order: 2; }
.panels-swapped .panel-right { order: 1; }

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
