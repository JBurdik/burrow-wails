<template>
  <div class="ide-root" :class="{ 'is-web': isWeb, 'is-compact': compact }">
    <AnimatedBackdrop
      v-if="ui.activeTheme.animated && !ui.bgImageUrl"
      :key="ui.theme"
      v-bind="ui.activeTheme.animated"
      :floor="ui.activeTheme.vars['bg-base']"
    />
    <TitleBar
      v-if="!compact"
      :workspace-name="displayWs?.name"
      :branch="git.cwd === displayWs?.path ? git.branch : ''"
      :folder-path="displayWs?.path"
      :right-panel-visible="ui.rightPanelVisible"
      :sidebar-visible="!compact && ui.sidebarVisible"
      @toggle-sidebar="ui.toggleSidebar()"
      @back="ws.close()"
      @toggle-rightpanel="ui.toggleRightPanel()"
    />
    <Settings v-if="ui.settingsOpen" @close="ui.closeSettings()" />
    <Onboarding v-if="onboardingOpen" @complete="completeOnboarding" @skip="completeOnboarding" />
    <DocsScreen
      v-if="docsOpen"
      @close="docsOpen = false"
      @onboarding="openOnboarding"
      @shortcuts="openShortcutsFromDocs"
    />
    <!-- Phone shell (src/components/phone/, docs/superpowers/specs/2026-10-22-phone-shell-design.md):
         the same panels, one per screen, chosen by the route; headers sit here. -->
    <template v-if="compact">
      <PhoneHeader v-if="phone === 'threads'" title="Threads">
        <template #actions>
          <button class="phone-icon-btn" aria-label="Search" @click="ui.openSpotlight()"><PhMagnifyingGlass :size="21" /></button>
          <button class="phone-icon-btn" aria-label="New thread" @click="ui.openWelcome()"><PhPlus :size="22" /></button>
        </template>
      </PhoneHeader>
      <ThreadHeader v-else-if="phone === 'thread'" />
      <PhoneHeader v-else-if="phone === 'project-surface'" :title="surfaceTitle" :subtitle="ws.active?.name" back />
    </template>
    <div class="ide-body" :class="{ 'panels-swapped': ui.swapPanels }" :style="panelStyles">
      <!-- Wrapped: Sidebar has several root nodes (Teleports), so v-show/class on the
           component itself silently did nothing and the sidebar never hid. -->
      <div v-show="compact ? phone === 'threads' : ui.sidebarVisible" class="panel-sidebar flex shrink-0"><Sidebar /></div>
      <div v-show="!compact && ui.sidebarVisible" class="resize-handle panel-resize-left" @mousedown="startResize('left', $event)" />
      <div v-show="!compact || phone === 'thread'" class="ide-main">
        <!-- Terminals stay MOUNTED across every mode (v-show, not v-if). Unmounting
             them on a mode switch ran XTerm.onBeforeUnmount → detach_pty + dispose;
             returning replayed the daemon ring-buffer into a fresh fit → scrambled
             buffer. Keeping them mounted avoids the detach/reattach entirely. -->
        <div v-show="ui.mode === 'terminal'" class="terminal-host">
          <WelcomeScreen ref="welcomeEl" v-show="showWelcome" @open-folder="openNewWorkspace" />
          <!-- ⌘J on the welcome screen: no thread to hang a panel on, so one shared
               panel lives here. Kept mounted (v-show) so its shells survive leaving. -->
          <BottomTerminalPanel
            v-if="welcomeBottom.ptyIds.length"
            v-show="showWelcome && welcomeBottom.open"
            :state="welcomeBottom"
            :cwd="welcomeBottomCwd"
            class="shrink-0"
            :style="{ height: welcomeBottom.height + 'px' }"
            @add="addWelcomeBottomTerminal"
            @close="closeWelcomeBottomTerminal"
            @select="(id) => welcomeBottom.activeId = id"
          />
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
      <InboxScreen v-if="compact && phone === 'inbox'" />
      <ProjectScreen v-else-if="compact && phone === 'project'" />
      <MoreScreen v-else-if="compact && phone === 'more'" />
      <div v-show="!compact && ui.rightPanelVisible" class="resize-handle panel-resize-right" @mousedown="startResize('right', $event)" />
      <RightPanel
        ref="rightPanelRef"
        class="panel-right"
        :class="{ 'phone-hidden': compact && phone !== 'project-surface' }"
        :open="compact ? phone === 'project-surface' : ui.rightPanelVisible"
        :force-surface="compact && phone === 'project-surface' ? String(route.params.surface) : undefined"
        :cwd="displayWs?.path ?? ''"
        :workspace-id="displayWs?.id"
        :is-git="displayWs?.is_git !== false"
        @open-panel="ui.rightPanelVisible = true"
        @close-panel="ui.rightPanelVisible = false"
        @open-project-config="showProjectConfig = true"
      />
    </div>
    <WorkspaceConfig
      v-if="showProjectConfig && ws.active"
      :workspace-id="ws.active.parent_id ?? ws.active.id"
      @close="showProjectConfig = false"
    />
    <BottomNav v-if="compact && showsBottomNav(route.name)" />
    <FileDropOverlay />
    <Spotlight
      ref="spotlightRef"
      @launch="(cmd) => activeTerm()?.spawnAgent(cmd)"
      @new-terminal="activeTerm()?.addTab()"
      @open-project-config="showProjectConfig = true"
      @open-browser="activeTerm()?.openBrowserTab()"
      @repaint="activeTerm()?.repaintAll()"
      @split-terminal="activeTerm()?.splitPane('h')"
      @open-file="openSearchHit"
    />
    <PathPicker />
    <ToastStack />
    <PtyPermissionToasts />
    <!-- Docked in the sidebar while it shows; floating only when it is hidden (⌘B). -->
    <UpdateBanner v-if="!ui.sidebarVisible" />
    <ProviderUpdateBanner v-if="!ui.sidebarVisible" />
    <DiagramModal v-if="diagramContent !== null" />
    <ImageLightbox v-if="lightboxSrc !== null" />
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
import { ref, reactive, onMounted, onBeforeUnmount, computed, provide, watch, nextTick, useTemplateRef } from "vue";
import { useRoute } from "vue-router";
import { router, routeId } from "@/router";
import { PhMagnifyingGlass, PhPlus, PhX } from "@phosphor-icons/vue";
import TitleBar from "@/components/TitleBar.vue";
import PhoneHeader from "@/components/phone/PhoneHeader.vue";
import ThreadHeader from "@/components/phone/ThreadHeader.vue";
import BottomNav from "@/components/phone/BottomNav.vue";
import InboxScreen from "@/components/phone/InboxScreen.vue";
import ProjectScreen from "@/components/phone/ProjectScreen.vue";
import MoreScreen from "@/components/phone/MoreScreen.vue";
import { isPhoneOnlyRoute, phoneScreen, showsBottomNav } from "@/lib/phoneNav";
import { compact, isWeb } from "@/lib/viewport";
import AnimatedBackdrop from "@/components/AnimatedBackdrop.vue";
import Sidebar from "@/components/Sidebar.vue";
import Terminal from "@/components/Terminal.vue";
import RightPanel from "@/components/RightPanel.vue";
import Dashboard from "@/components/Dashboard.vue";
import Settings from "@/components/Settings.vue";
import Spotlight from "@/components/Spotlight.vue";
import FileDropOverlay from "@/components/FileDropOverlay.vue";
import UpdateBanner from "@/components/UpdateBanner.vue";
import ProviderUpdateBanner from "@/components/ProviderUpdateBanner.vue";
import ToastStack from "@/components/ToastStack.vue";
import PtyPermissionToasts from "@/components/PtyPermissionToasts.vue";
import PetOverlay from "@/components/PetOverlay.vue";
import WorkspaceConfig from "@/components/WorkspaceConfig.vue";
import WelcomeScreen from "@/components/WelcomeScreen.vue";
import BottomTerminalPanel from "@/components/BottomTerminalPanel.vue";
import { nextPtyId, initPtyCounter } from "@/lib/ptyId";
import Onboarding from "@/components/Onboarding.vue";
import DocsScreen from "@/components/DocsScreen.vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useGitStore } from "@/stores/git";
import { useProvidersStore } from "@/stores/providers";
import { useUpdateStore } from "@/stores/update";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { matchesShortcut } from "@/lib/shortcuts";
import { useKeybindingsStore } from "@/stores/keybindings";
import { useChatAttentionStore } from "@/stores/chatAttention";
import { FIXED_SHORTCUTS } from "@/lib/keymap";
import PathPicker from "@/components/PathPicker.vue";
import { pickDir } from "@/lib/pickPath";
import { dirOf, looksLikeMdPath, resolveLocalLink } from "@/lib/localLink";
import { installControlBridge } from "@/lib/controlBridge";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import DiagramModal from "@/components/DiagramModal.vue";
import ImageLightbox from "@/components/ImageLightbox.vue";
import { useDiagram } from "@/composables/useDiagram";
import { useImageLightbox } from "@/composables/useImageLightbox";
import { configReady, getConfig, setConfig } from "@/lib/config";

let resizing: 'left' | 'right' | null = null;
let resizeStartX = 0;
let resizeStartWidth = 0;

const showProjectConfig = ref(false);
const rightPanelRef = useTemplateRef<InstanceType<typeof RightPanel>>("rightPanelRef");

const ws = useWorkspaceStore();
const ui = useUIStore();
const git = useGitStore();
const { diagramContent } = useDiagram();
const { lightboxSrc } = useImageLightbox();
const providers = useProvidersStore();
const keys = useKeybindingsStore();
const update = useUpdateStore();
const tabsStore = useTerminalTabsStore();
// App-wide on purpose: every chat dot (Sidebar, tabs, Dashboard) reads
// session.status, which this store derives from the Go phase.
useChatAttentionStore();
const onboardingOpen = ref(false);
const docsOpen = ref(false);
const ONBOARDING_CONFIG_KEY = "onboardingComplete";

configReady.then(() => {
  onboardingOpen.value = !getConfig<boolean>(ONBOARDING_CONFIG_KEY, false);
});

function completeOnboarding() {
  onboardingOpen.value = false;
  setConfig(ONBOARDING_CONFIG_KEY, true);
}
function openOnboarding() {
  docsOpen.value = false;
  onboardingOpen.value = true;
}
function openShortcutsFromDocs() {
  docsOpen.value = false;
  cheatsheetOpen.value = true;
}

// Tabs of the active workspace (the Sidebar's shelves read them from here).
const route = useRoute();

// `undefined` means that workspace's Terminal has not synced its tabs yet (it
// may have been mounted a tick ago); `[]` means it really has none. Collapsing
// the two is what made the watcher below fire on a workspace that was merely
// still loading.
const activeTabs = computed(() => (ws.active ? tabsStore.tabsByWs[ws.active.id] : undefined));

// The view state is the ROUTE (fáze 4, docs/plans/003-view-state-routes.md).
// It used to be a computed local to App.vue, invisible to Terminal.vue's
// watched/seen logic — which is how a tab hidden behind the composer counted as
// watched. The ui store now only reads the route back out.
const showWelcome = computed(() => ui.welcomeVisible);

// ── route ⇄ stores ───────────────────────────────────────────────────────────
// The URL names the workspace and tab; the stores hold the objects. These two
// watchers keep them in step, each guarded so it never answers the other.
//
// ponytail: no navigation guards, no router-view. Two watchers in the shell are
// enough for a single-window desktop app, and they stay readable — a guard
// would have to wait for the workspace list to load before it could resolve
// /ws/7 at all.
watch(
  () => route.fullPath,
  async () => {
    const wsId = routeId(route.params.wsId);
    if (wsId === null) return;
    if (ws.active?.id !== wsId) {
      const target = ws.workspaces.find((w) => w.id === wsId) ?? ws.opened.find((w) => w.id === wsId);
      // Unknown id (a stale deep link) — say so by going to the composer
      // rather than showing whatever happened to be active.
      if (!target) { void router.replace("/"); return; }
      ws.open(target);
    }
    const tabId = routeId(route.params.tabId);
    if (tabId !== null) {
      // Chat tabs get a fresh numeric id every time Terminal (re)creates them
      // (nextPtyId(), shared with real PTYs) — tabId alone can point at the
      // wrong thread once Terminal has restored/remounted since this route was
      // last visited. Pass the stable chatId too, when the mirror still knows
      // it, so Terminal can re-resolve the current tab for that thread.
      const chatId = tabsStore.tabsByWs[wsId]?.find((t) => t.id === tabId)?.chatId;
      tabsStore.activate(wsId, tabId, chatId);
    }
  },
  { immediate: true },
);

watch(
  () => ws.active?.id,
  (id) => {
    // Closing the workspace (TitleBar back) leaves nothing addressable.
    if (id == null) {
      if (route.name === "workspace" || route.name === "tab") void router.replace("/");
      return;
    }
    // Only re-address routes that name a workspace; being on /dashboard or the
    // composer is a deliberate place to be, not a stale URL.
    if (route.name !== "workspace" && route.name !== "tab") return;
    if (routeId(route.params.wsId) === id) return;
    void router.replace(`/ws/${id}`);
  },
);

// Back to auto once the workspace is empty, so closing the last tab brings the
// composer back after an earlier dismissal.
// The last live tab closing means there is nothing left to look at, so the
// composer becomes the honest destination rather than an empty terminal host.
// Only within ONE workspace, though: a switch to a workspace that has not
// reported its tabs yet is not an empty workspace, and treating it as one sent
// the user to the composer every time something opened another workspace
// underneath them (a `burrow spawn` from an agent running elsewhere).
watch(
  () => [ws.active?.id ?? null, activeTabs.value?.length ?? null] as const,
  // On a phone the thread list, not a composer, is where an emptied workspace leaves you.
  ([id, n], [prevId]) => { if (id === prevId && n === 0) void (compact.value ? router.replace("/") : ui.openWelcome()); },
);

// Screen stays mounted behind v-show, so re-focus its composer each time it shows.
// Welcome-screen bottom terminal panel (see template). ponytail: fixed height,
// no resize handle — copy Terminal.vue's startBottomResize if it's wanted here.
const welcomeBottom = reactive({ open: false, ptyIds: [] as number[], activeId: null as number | null, height: 220 });
// Fixed at first open: a pty's cwd is only read at spawn anyway.
const welcomeBottomCwd = ref("");
async function addWelcomeBottomTerminal() {
  // No Terminal.vue may have restored sessions yet (nothing opened), so sync the
  // shared id counter with the daemon first — reusing a live id attaches to it.
  const sessions = await invoke<Array<{ pty_id: number }>>("list_pty_sessions").catch(() => []);
  initPtyCounter(sessions.reduce((max, s) => s.pty_id < 1_000_000 ? Math.max(max, s.pty_id) : max, 0));
  const id = nextPtyId();
  welcomeBottom.ptyIds.push(id);
  welcomeBottom.activeId = id;
}
function closeWelcomeBottomTerminal(id: number) {
  void invoke("kill_pty", { id }).catch(() => {});
  welcomeBottom.ptyIds = welcomeBottom.ptyIds.filter((p) => p !== id);
  if (welcomeBottom.activeId === id) welcomeBottom.activeId = welcomeBottom.ptyIds[welcomeBottom.ptyIds.length - 1] ?? null;
  if (!welcomeBottom.ptyIds.length) welcomeBottom.open = false;
}
function toggleWelcomeBottom() {
  welcomeBottom.open = !welcomeBottom.open;
  if (!welcomeBottom.open || welcomeBottom.ptyIds.length) return;
  welcomeBottomCwd.value = (welcomeEl.value?.target ?? ws.active)?.path ?? "";
  void addWelcomeBottomTerminal();
}

const welcomeEl = useTemplateRef<{ focus: () => void; cycleProvider: () => void; target: Workspace | null }>("welcomeEl");
watch(showWelcome, (on) => { if (on) nextTick(() => welcomeEl.value?.focus()); });

// While the welcome screen is up, the titlebar/right-panel git surfaces follow
// whatever project it's targeting (its own project picker, not ws.active —
// picking there doesn't open the workspace until the first message is sent).
const displayWs = computed<Workspace | null>(() =>
  showWelcome.value ? (welcomeEl.value?.target ?? ws.active) : ws.active,
);

const panelStyles = computed(() => compact.value
  // auto + the fixed insets below size them; vw would be scaled again by the UI zoom on #app.
  ? { '--sidebar-width': 'auto', '--right-panel-width': 'auto' }
  : { '--sidebar-width': ui.sidebarWidth + 'px', '--right-panel-width': ui.rightPanelWidth + 'px' });

// Phone shell: which screen the route shows (src/lib/phoneNav.ts). The thread
// list is the Sidebar, a project section is the RightPanel with one surface
// forced, a thread is the terminal host — the same mounted panels, shown one
// at a time.
const phone = computed(() => phoneScreen(route.name));
const SURFACE_TITLES: Record<string, string> = { git: "Changes", "pull-requests": "Pull requests", "dev-servers": "Dev servers", agents: "Sub-agents" };
const surfaceTitle = computed(() => SURFACE_TITLES[String(route.params.surface)] ?? "Project");
// Widening past phone width on a phone-only screen (rotation, split view):
// the desktop has nothing to show there.
watch(compact, (on) => { if (!on && isPhoneOnlyRoute(route.name)) void router.replace("/"); });
const closeRightOnCompact = () => { if (compact.value) ui.rightPanelVisible = false; };
watch(compact, closeRightOnCompact);
configReady.then(closeRightOnCompact); // after the ui store restored its prefs

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

function openRightPanelGitTab() {
  rightPanelRef.value?.openGitTab();
}

provide('openRightPanelGitTab', openRightPanelGitTab);

// A transcript row that names a sub-agent chat is a pointer into the panel,
// not a mini-view of its own — clicking it opens (and shows the panel) the
// same way the git-tab shortcut above does.
function openRightPanelSubagent(chatId: number, workspaceId: number) {
  ui.rightPanelVisible = true;
  rightPanelRef.value?.openSubagent(chatId, workspaceId);
}

provide('openRightPanelSubagent', openRightPanelSubagent);

// Same for a transcript row that names a panel surface (a background task
// started, a plan created, files changed): open it in the chat's workspace.
function openRightPanelSurface(surface: string, workspaceId: number) {
  ui.rightPanelVisible = true;
  rightPanelRef.value?.openPanelSurface(surface, workspaceId);
}

provide('openRightPanelSurface', openRightPanelSurface);

// A file link in rendered markdown (chat, an open document): markdown opens
// rendered in the panel, anything else in the editor. Without this a relative
// href navigates the whole WKWebView away. External URLs are main.ts's job.
// Existence is checked against the parent dir listing (cached per dir) so a
// stray `foo.ts:12` in prose that isn't a real file does nothing.
const dirListings = new Map<string, Promise<Set<string>>>();
function fileExists(path: string): Promise<boolean> {
  const dir = dirOf(path);
  let listing = dirListings.get(dir);
  if (!listing) {
    listing = invoke<{ name: string; isDir: boolean }[]>("read_dir_shallow", { path: dir })
      .then((es) => new Set(es.filter((e) => !e.isDir).map((e) => e.name)))
      .catch(() => new Set<string>());
    dirListings.set(dir, listing);
    setTimeout(() => dirListings.delete(dir), 30_000);
  }
  return listing.then((names) => names.has(path.slice(dir.length + 1)));
}

function openLocalFile(link: NonNullable<ReturnType<typeof resolveLocalLink>>) {
  if (link.line !== undefined) {
    // A line reference lands in the Right Panel editor, highlighted.
    ui.rightPanelVisible = true;
    rightPanelRef.value?.openFile(link.path, { from: link.line, to: link.lineEnd ?? link.line });
  } else if (link.markdown) rightPanelRef.value?.openDoc(link.path);
  else activeTerm()?.openFileInTab(link.path, link.path.split("/").pop() ?? link.path);
}

// A file link in rendered markdown (chat, an open document): markdown opens
// rendered in the panel, anything else in the editor. Without this a relative
// href navigates the whole WKWebView away. External URLs are main.ts's job.
function onLocalLinkClick(e: MouseEvent) {
  const target = e.target as Element | null;
  const scope = target?.closest?.(".md-body, [data-link-base]");
  const root = ws.active?.path;
  if (!scope || !root) return;
  const base = scope.closest("[data-link-base]")?.getAttribute("data-link-base") || root;
  const ref = target!.closest("[data-file-ref]");
  if (ref) {
    // Linkified `path:line` text: no href, so there is nothing to cancel.
    const link = resolveLocalLink(ref.getAttribute("data-file-ref"), base);
    if (!link) return;
    e.stopPropagation();
    void fileExists(link.path).then((ok) => { if (ok) openLocalFile(link); });
    return;
  }
  const a = target!.closest("a[href]");
  const code = !a && target!.closest("code");
  const raw = a ? a.getAttribute("href") : code && !code.closest("pre") && looksLikeMdPath(code.textContent ?? "") ? code.textContent : null;
  const link = resolveLocalLink(raw, base);
  if (!link) return;
  e.preventDefault();
  e.stopPropagation();
  openLocalFile(link);
}


async function openNewWorkspace() {
  const dir = await pickDir({ title: "Add project", start: ui.defaultProjectDir || "~/" });
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
let unlistenMenuDocs: UnlistenFn | null = null;
let unlistenMenuOnboarding: UnlistenFn | null = null;
let unlistenMenuShortcuts: UnlistenFn | null = null;
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
  await ws.load();
  // Startup lands on the composer ("/"), never on the last session's thread —
  // reopening the app is a fresh start, and restoring a thread also left it
  // focused in the sidebar with nothing the user asked for behind it.
  if (route.path === "/" && ui.startupMode === "dashboard") void router.replace("/dashboard");
  playStartupChime();
  window.addEventListener("keydown", onKeydown);
  document.addEventListener("click", onLocalLinkClick, true);
  window.addEventListener('mousemove', onResizeMove);
  window.addEventListener('mouseup', onResizeUp);
  setTimeout(() => update.check({ silent: true }), 3000);
  updateTimer = window.setInterval(() => update.check({ silent: true }), 6 * 60 * 60 * 1000);
  setTimeout(() => providers.probeAll().then(() => providers.checkLatest()), 3000);

  unlistenMenuUpdate = await listen("menu-check-update", () => {
    update.check({ silent: false });
  });
  unlistenMenuDocs = await listen("menu-open-docs", () => { docsOpen.value = true; });
  unlistenMenuOnboarding = await listen("menu-show-onboarding", () => { onboardingOpen.value = true; });
  unlistenMenuShortcuts = await listen("menu-show-shortcuts", () => { cheatsheetOpen.value = true; });

  unlistenWorkspacesChanged = await listen("workspaces-changed", () => {
    ws.load();
  });

  // The control API's UI half: one app-wide listener performing the actions
  // agents ask for, and acking each with its result.
  unlistenControl = await installControlBridge();
});
onBeforeUnmount(() => {
  // Same as Terminal.vue's bottom panels: not persisted, so nothing would ever
  // reattach — leaving them alive orphans a process in the daemon for good.
  welcomeBottom.ptyIds.forEach((id) => void invoke("kill_pty", { id }).catch(() => {}));
  window.removeEventListener("keydown", onKeydown);
  document.removeEventListener("click", onLocalLinkClick, true);
  window.removeEventListener('mousemove', onResizeMove);
  window.removeEventListener('mouseup', onResizeUp);
  if (updateTimer) clearInterval(updateTimer);
  unlistenMenuUpdate?.();
  unlistenMenuDocs?.();
  unlistenMenuOnboarding?.();
  unlistenMenuShortcuts?.();
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
      tabsStore.activate(Number(wsId), reviewTab.id, reviewTab.chatId);
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
  if (showWelcome.value && keys.matches(e, "bottomPanel")) {
    e.preventDefault();
    toggleWelcomeBottom();
    return;
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
@import "@/styles/chat-blocks.css";

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
  /* Plum palette (warm plum-black chrome, lavender accent).
     Mirrors themes.ts's "plum" entry (DEFAULT_THEME_KEY) — this block is only
     the pre-JS paint; stores/ui.ts's applyTheme() overwrites these same
     `--${key}` custom properties from the active Theme on mount/switch. */
  --bg-base: #241A20;
  --terminal-bg: #1E161B;
  --bg-panel: #2C2127;
  --bg-dropdown: #2C2127;
  --bg-hover: #352A30;
  --bg-selected: #3A2E34;
  --border: #3D3137;
  --text-primary: #ECE6E9;
  --text-secondary: #A79CA2;
  --text-muted: #7E7379;
  --accent: #A895F5;
  --accent-dim: #7C6FD8;
  --green: #7FBF8E;
  --yellow: #F2C14E;
  --red: #D9827A;
  --font-mono: "JetBrains Mono", "0xProto Nerd Font", "Fira Code", "Cascadia Code", monospace;
  --font-ui: "JetBrains Mono", "0xProto Nerd Font", "Fira Code", monospace;
  --sidebar-width: 240px;
  /* The sidebar column (and the titlebar strip above it) sits a step darker than the work area. */
  --sidebar-bg: color-mix(in srgb, var(--bg-base) 88%, black);
  --right-panel-width: 300px;
  --titlebar-height: 36px;

  /* ── Visual-language aliases (pen.dev card/bubble/chip spec) ──
     Named to match the design tokens directly (surface / surface-elevated /
     text-tertiary) while staying theme-aware: they reference the existing
     Theme vars above rather than hardcoding a color, so every one of the
     17 themes in themes.ts gets a sensible value for free and switching
     themes (stores/ui.ts's applyTheme) never needs to know these names exist. */
  --surface: var(--bg-panel);
  --surface-elevated: var(--bg-hover);
  --text-tertiary: var(--text-muted);

  /* Status-blue: "done"/"review" render blue in the new language, distinct
     from the purple accent and from status-dots.css's running/waiting/error
     colors (which key off --status-*, not --blue). */
  --blue: #60A5FA;

  /* Shape tokens: 16px cards/bubbles, 10-14px buttons/chips, pill badges. No
     drop shadows anywhere in this language — flat, bordered, fill-only. */
  --radius-card: 16px;
  --radius-bubble: 16px;
  --radius-composer: 20px;
  --radius-chip: 14px;
  --radius-nav: 10px;
  --radius-avatar: 10px;
  --radius-pill: 999px;
}

/* Animated themes (ui.ts applyTheme sets data-animated): surfaces are
   translucent over the backdrop canvas, so drop every extra layer that would
   stack more cover on top — the glow mixes into nothing, split gaps go clear,
   and Settings (a fixed overlay with its own canvas) sits on the opaque floor
   instead of letting the app show through. */
:root[data-animated] .surface-glow { --glow-base: transparent; }
:root[data-animated] { --sidebar-bg: var(--bg-base); }
:root[data-animated] .terminal-tab-content { background: transparent; }
:root[data-animated] .settings-root { background-color: var(--anim-floor); }
/* Full-screen overlays without their own canvas: solid, or the app shows through. */
:root[data-animated] .docs-screen,
:root[data-animated] .onboarding { background: var(--anim-floor); }
/* Anything floating over content (menus, pickers, popovers, Spotlight,
   dialogs, banners) uses --bg-dropdown, never --bg-panel: applyTheme keeps
   it opaque under animated themes and wallpapers. */

/* Subtle glow on the big surfaces (sidebar, chat, welcome, dashboard, RP),
   derived from the active theme's accent so every theme gets it for free;
   a theme can override color + strength (Theme.glow → --glow-color/-strength).
   Opaque: every stop is the accent mixed into the surface's own bg
   (--glow-base, set on the surface; inherited, so a nested surface sets its
   own), so nothing composites over anything.
   The falloff is a raised cosine sampled into many stops — CSS interpolates
   linearly between stops and every kink in the slope reads as a band; the
   cosine has none and reaches the base with zero slope. Declared on the
   class, not :root, so var(--glow-base) resolves per element. */
.surface-glow {
  background-image: radial-gradient(120% 70% at 0% 0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 1.000), var(--glow-base, var(--bg-base))) 0.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.983), var(--glow-base, var(--bg-base))) 5.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.933), var(--glow-base, var(--bg-base))) 10.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.853), var(--glow-base, var(--bg-base))) 15.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.750), var(--glow-base, var(--bg-base))) 20.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.630), var(--glow-base, var(--bg-base))) 25.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.500), var(--glow-base, var(--bg-base))) 30.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.370), var(--glow-base, var(--bg-base))) 35.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.250), var(--glow-base, var(--bg-base))) 40.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.147), var(--glow-base, var(--bg-base))) 45.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.067), var(--glow-base, var(--bg-base))) 50.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.017), var(--glow-base, var(--bg-base))) 55.0%,
    color-mix(in srgb, var(--glow-color, var(--accent)) calc(var(--glow-strength, 7%) * 0.000), var(--glow-base, var(--bg-base))) 60.0%);
}

/* Quiet one-line notices docked at the bottom of the sidebar (app / agent
   updates): the whole row is the action, the × only shows on hover. */
.sb-notice { display: flex; width: 100%; align-items: center; gap: 9px; padding: 7px 8px 7px 10px; border-radius: 9px; text-align: left; cursor: pointer; background: color-mix(in srgb, var(--text-primary) 4%, transparent); transition: background-color .12s; }
.sb-notice:hover { background: color-mix(in srgb, var(--text-primary) 8%, transparent); }
.sb-notice-x { display: flex; flex-shrink: 0; padding: 3px; border-radius: 5px; color: var(--text-muted); opacity: 0; transition: opacity .12s; }
.sb-notice:hover .sb-notice-x { opacity: 1; }
.sb-notice-x:hover { color: var(--text-primary); background: color-mix(in srgb, var(--text-primary) 8%, transparent); }

body {
  background-color: var(--bg-base);
  /* Meme themes set --bg-image to a tiled wallpaper; normal themes = none. */
  background-image: var(--bg-image, none);
  background-attachment: fixed;
  /* Own stacking context, so AnimatedBackdrop's z-index:-1 canvas paints above this
     background and below every surface (without it the canvas sat under it). */
  isolation: isolate;
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

/* Browser clients (iPad, phone): no traffic lights, no "open on this Mac",
   and the bar clears the status bar of a home-screen web app. */
.is-web { --titlebar-height: calc(40px + env(safe-area-inset-top, 0px)); }
.is-web .titlebar { padding-top: env(safe-area-inset-top, 0px); }
.is-web .ide-body { padding-bottom: env(safe-area-inset-bottom, 0px); }
.is-web .tb-traffic,
.is-web .tb-web-hide,
.is-compact .tb-wide { display: none !important; }

/* Phone shell: one in-flow panel per screen, no floating drawers. */
.is-compact .ide-body { flex-direction: column; }
.is-compact .panel-sidebar,
.is-compact .panel-right { flex: 1 1 0; min-height: 0; width: 100%; background: var(--bg-base); }
.is-compact .panel-sidebar > aside,
.is-compact .panel-right { width: 100% !important; flex-basis: 100% !important; min-width: 0; border-left: 0; }
.is-compact:has(.phone-nav) .ide-body { padding-bottom: 0; }
/* Pinned to the visual viewport (src/mobile/main.ts sets --app-top/--app-h),
   so the iOS keyboard shrinks the app instead of covering the composer. */
.is-web #app { position: fixed; left: 0; right: 0; top: var(--app-top, 0px); width: auto; height: var(--app-h, 100dvh); }
/* No title bar on a phone; overlays that sit below it start at the notch. */
.is-compact { --titlebar-height: env(safe-area-inset-top, 0px); }
.is-compact .phone-hidden { display: none !important; }
/* A detail slides in when it appears (display:none → shown restarts the
   animation); back has no animation, iOS swipe-back draws its own. */
.is-compact .ide-main,
.is-compact .panel-right { animation: phone-push-in 0.22s ease-out; }
@keyframes phone-push-in { from { transform: translateX(28%); opacity: 0.6; } to { transform: none; opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .is-compact .ide-main, .is-compact .panel-right { animation: none; } }
.phone-icon-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: 44px; height: 44px; flex-shrink: 0; border-radius: 10px;
  color: var(--text-primary);
}
.phone-icon-btn:active { background: var(--bg-hover); }
/* Phone chat: nothing may widen the page — wide code blocks scroll inside
   themselves — and the desk-only chrome (minimap rail, "Catch me up") goes. */
.is-compact .chat-messages { overflow-x: hidden; }
.is-compact .chat-messages pre { max-width: 100%; overflow-x: auto; }
.is-compact .chat-messages .md-body { min-width: 0; overflow-wrap: anywhere; }
.is-compact .chat-message .bubble-user { max-width: 88%; }
.is-compact .minimap-rail { display: none; }
.is-compact .catchup-btn { display: none; }

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
