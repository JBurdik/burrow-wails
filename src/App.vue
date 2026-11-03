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
      :folder-path="displayWs?.path"
      :right-panel-visible="ui.rightPanelVisible"
      :sidebar-visible="!compact && ui.sidebarVisible"
      :details-open="!!details"
      @toggle-details="toggleDetails"
      @toggle-bottom="toggleBottomTerminal"
      @toggle-sidebar="ui.toggleSidebar()"
      @back="ws.close()"
      @toggle-rightpanel="rpPopped ? openRightPanelWindow() : ui.toggleRightPanel()"
    />
    <Settings v-if="ui.settingsOpen" @close="ui.closeSettings()" />
    <Onboarding v-if="onboardingOpen" @complete="completeOnboarding" @skip="completeOnboarding" />
    <DocsScreen
      v-if="docsOpen"
      @close="docsOpen = false"
      @onboarding="openOnboarding"
      @shortcuts="openShortcutsFromDocs"
    />
    <HtmlPageModal
      v-if="whatsNew"
      :title="`What's new in v${whatsNew.version}`"
      path=""
      :html="whatsNew.html"
      @close="whatsNew = null"
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
      <div v-show="!compact || phone === 'thread'" ref="mainEl" class="ide-main" :class="{ 'details-reserve': details?.kind === 'chat' && !details.overlay }" :style="{ '--details-inset': (detailsInset ?? 0) + 'px' }">
        <!-- Terminals stay MOUNTED across every mode (v-show, not v-if). Unmounting
             them on a mode switch ran XTerm.onBeforeUnmount → detach_pty + dispose;
             returning replayed the daemon ring-buffer into a fresh fit → scrambled
             buffer. Keeping them mounted avoids the detach/reattach entirely. -->
        <div v-show="ui.mode === 'terminal'" class="terminal-host">
          <WelcomeScreen ref="welcomeEl" v-show="showWelcome" @open-folder="openNewWorkspace" />
          <Terminal
            v-for="w in mountedWorkspaces"
            v-show="ws.active && w.id === ws.active.id && !showWelcome"
            :key="w.id"
            :workspace-id="w.id"
            :cwd="w.path"
            :ref="(el) => setTermRef(w.id, el)"
          />
        </div>
        <Transition name="details-card">
        <ThreadDetailsCard
          v-if="details"
          :class="{ 'details-overlay': details.overlay }"
          :workspace="details.workspace"
          :kind="details.kind"
          :chat-id="details.chatId"
          @close="closeDetails"
          @open-config="showProjectConfig = true"
          @open-pr="(n, url) => { ui.rightPanelVisible = true; rightPanelRef?.openPullRequest(n, url, details!.workspace.id); }"
        />
        </Transition>
        <Dashboard v-if="ui.mode === 'dashboard'" class="dashboard-main-panel" @new-workspace="openNewWorkspace" />
      </div>
      <InboxScreen v-if="compact && phone === 'inbox'" />
      <ProjectScreen v-else-if="compact && phone === 'project'" />
      <MoreScreen v-else-if="compact && phone === 'more'" />
      <div v-show="!compact && ui.rightPanelVisible && !rpPopped" class="resize-handle panel-resize-right" @mousedown="startResize('right', $event)" />
      <!-- v-if on rpPopped: the panel lives in its own window then, and two
           panels would attach the same terminal PTYs (rpWindow.ts). -->
      <RightPanel
        v-if="!rpPopped"
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
        @pop-out="popOutRightPanel"
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
import "@/styles/app.css";
import { isDesktop } from "@/lib/wailsCompat/desktop";
import { startCoreLink } from "@/lib/coreLink";
import { ref, onMounted, onBeforeUnmount, computed, provide, watch, nextTick, useTemplateRef } from "vue";
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
import ThreadDetailsCard from "@/components/ThreadDetailsCard.vue";
import { detailsLaneInset } from "@/lib/threadDetails";
import Onboarding from "@/components/Onboarding.vue";
import DocsScreen from "@/components/DocsScreen.vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
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
import { classifyChatLink, localUrl, prRef, repoKey } from "@/lib/chatLinks";
import { open as shellOpen } from "@tauri-apps/plugin-shell";
import { installControlBridge } from "@/lib/controlBridge";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { openWindow } from "@/lib/windows";
import { RP_WINDOW_KEY, publishFollow, remotePanel } from "@/lib/rpWindow";
import { installPanelTerminals } from "@/lib/rpTerminals";
import { activeChatIdFor } from "@/stores/chatTree";
import DiagramModal from "@/components/DiagramModal.vue";
import ImageLightbox from "@/components/ImageLightbox.vue";
import HtmlPageModal from "@/components/HtmlPageModal.vue";
import { pendingReleaseNotes, allReleaseNotes } from "@/lib/whatsNew";
import { hideSplash } from "@/lib/splash";
import { useDiagram } from "@/composables/useDiagram";
import { useImageLightbox } from "@/composables/useImageLightbox";
import { configReady, getConfig, setConfig } from "@/lib/config";
import { useNotificationsStore } from "@/stores/notifications";

let resizing: 'left' | 'right' | null = null;
let resizeStartX = 0;
let resizeStartWidth = 0;

const showProjectConfig = ref(false);
const rightPanelRef = useTemplateRef<InstanceType<typeof RightPanel>>("rightPanelRef");

const ws = useWorkspaceStore();
const ui = useUIStore();
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
const whatsNew = ref<{ version: string; html: string } | null>(null);
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
// A remote client restores every workspace the desktop has open (config.json
// is shared) and would mount all their terminals, each replaying up to 1 MiB of
// scrollback; iOS kills the page for that (white screen). The desktop keeps
// them mounted because a re-attach corrupts the scrollback — a phone pays that
// for one remount instead.
const mountedWorkspaces = computed(() =>
  isWeb ? ws.opened.filter((w) => w.id === ws.active?.id) : ws.opened,
);

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
    // ?chat=<id> is a push notification's deep link (src-wails/pushtrigger.go):
    // chat tab ids are client-side, so Go can only name the chat. Opening it
    // routes on to its tab, which drops the query.
    const pushedChat = routeId(route.query.chat);
    if (pushedChat !== null) tabsStore.openChat(wsId, pushedChat);
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
// Thread details card: pinned open or closed per device. Desktop only — the
// phone shell has its own thread header — and only for something in a workspace.
const DETAILS_KEY = "burrow.threadDetailsOpen";
const detailsOpen = ref(localStorage.getItem(DETAILS_KEY) !== "0"); // open unless closed once
watch(detailsOpen, (on) => localStorage.setItem(DETAILS_KEY, on ? "1" : "0"));
// t3code's rule (chatCanvasLayout.ts): the card never covers the chat. The chat
// lane stays centred while it clears the card and steps left only as far as it
// must (detailsLaneInset); too narrow for that and the card hides instead.
const mainEl = ref<HTMLElement | null>(null);
const mainWidth = ref(Infinity);
const mainObserver = new ResizeObserver(([e]) => { if (e) mainWidth.value = e.contentRect.width; });
watch(mainEl, (el, old) => { if (old) mainObserver.unobserve(old); if (el) mainObserver.observe(el); });
onBeforeUnmount(() => mainObserver.disconnect());
const rawInset = computed(() => (Number.isFinite(mainWidth.value) ? detailsLaneInset(mainWidth.value) : 0));
// Docked = the card has a column of its own beside the chat lane. With the RP
// closed the lane may step left for it; with the RP open it docks only on a
// screen wide enough that the lane needn't move at all (inset 0) — the RP
// already took the room. Hiding is debounced: opening the RP can report one
// narrow transient width, and hide+reshow read as a blink.
const fitsNow = computed(() => rawInset.value !== null && (!ui.rightPanelVisible || rawInset.value === 0));
const detailsFits = ref(fitsNow.value);
const detailsInset = ref<number | null>(rawInset.value);
let hideTimer: ReturnType<typeof setTimeout> | undefined;
watch([fitsNow, rawInset], ([fits, inset]) => {
  clearTimeout(hideTimer);
  if (inset !== null) detailsInset.value = inset;
  if (fits) detailsFits.value = true;
  else hideTimer = setTimeout(() => { if (!fitsNow.value) detailsFits.value = false; }, 250);
});
// Not docked (no room, or the user closed it): the (i) button still opens the
// card, as a popover over the chat — the details are never out of reach.
const detailsPeek = ref(false);
const details = computed(() => {
  const w = ws.active;
  if (compact.value || showWelcome.value || ui.mode !== "terminal" || !w) return null;
  const docked = detailsOpen.value && detailsFits.value;
  if (!docked && !detailsPeek.value) return null;
  const tab = ui.viewingTabs ? tabsStore.tabsByWs[w.id]?.find((t) => t.id === tabsStore.activeByWs[w.id]) : undefined;
  const kind: "chat" | "tab" | "workspace" = !tab ? "workspace" : tab.chatId != null ? "chat" : "tab";
  return { workspace: w, kind, chatId: tab?.chatId, overlay: !docked };
});
function toggleDetails() {
  if (details.value) {
    if (details.value.overlay) detailsPeek.value = false;
    else detailsOpen.value = false;
  } else if (detailsFits.value) detailsOpen.value = true;
  else detailsPeek.value = true;
}
function closeDetails() {
  if (details.value?.overlay) detailsPeek.value = false;
  else detailsOpen.value = false;
}
// The popover closes on a click outside it (the toggle handles its own click).
function onPeekPointer(e: PointerEvent) {
  const t = e.target as Element | null;
  if (detailsPeek.value && !t?.closest(".details-card, [data-details-toggle]")) detailsPeek.value = false;
}
window.addEventListener("pointerdown", onPeekPointer, true);
onBeforeUnmount(() => window.removeEventListener("pointerdown", onPeekPointer, true));
// Room came back: the popover becomes the docked card again.
watch(detailsFits, (fits) => { if (fits && detailsPeek.value) { detailsPeek.value = false; detailsOpen.value = true; } });

// ⌘J / the titlebar button: the Right Panel's terminal. The RP follows
// displayWs, so on the welcome screen it is the targeted project's.
function toggleBottomTerminal() {
  if (rpPopped.value) return void openRightPanelWindow();
  void rightPanelRef.value?.toggleTerminal();
}

// ── The Right Panel in its own window (optional, its header's button) ─────────
// This window unmounts its panel and publishes what the panel should follow;
// closing that window brings the panel back here. See src/lib/rpWindow.ts.
const rpPopped = ref(false);
function openRightPanelWindow() {
  return openWindow(RP_WINDOW_KEY, "/window/rightpanel", "Right panel", 480, 820);
}
async function popOutRightPanel() {
  rpPopped.value = true;
  await nextTick(); // the panel is unmounted (its terminal list saved) before the window mounts one
  installPanelTerminals(remotePanel);
  await openRightPanelWindow().catch(() => { rpPopped.value = false; });
}
watch(rpPopped, (on) => { if (!on) installPanelTerminals(null); }); // the remounted panel installs itself
void listen<string>("window-closed", ({ payload }) => { if (payload === RP_WINDOW_KEY) rpPopped.value = false; });

const welcomeEl = useTemplateRef<{ focus: () => void; cycleProvider: () => void; target: Workspace | null }>("welcomeEl");
watch(showWelcome, (on) => { if (on) nextTick(() => welcomeEl.value?.focus()); });

// While the welcome screen is up, the titlebar/right-panel git surfaces follow
// whatever project it's targeting (its own project picker, not ws.active —
// picking there doesn't open the workspace until the first message is sent).
const displayWs = computed<Workspace | null>(() =>
  showWelcome.value ? (welcomeEl.value?.target ?? ws.active) : ws.active,
);
// What a popped-out Right Panel shows (rpWindow.ts); defined after displayWs,
// which the immediate getter reads.
watch(
  () => ({
    workspaceId: displayWs.value?.id,
    cwd: displayWs.value?.path ?? "",
    isGit: displayWs.value?.is_git !== false,
    chatId: displayWs.value ? activeChatIdFor(tabsStore.tabsByWs, tabsStore.activeByWs, displayWs.value.id) : null,
  }),
  (f) => publishFollow(f),
  { immediate: true, deep: true },
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

// A link in a chat transcript opens BESIDE the chat, like Claude Desktop: this
// repo's PR → the PRs surface on that PR, a localhost dev server → the Browser
// surface (src/lib/chatLinks.ts says why nothing else does). ⌘/Ctrl-click, a
// modified or middle click and every other link fall through to main.ts →
// system browser. On `window`, capturing: main.ts's handler sits on
// `document` and would otherwise open the system browser first.
const repoKeysByCwd = new Map<string, Promise<string[]>>();
function repoKeysFor(cwd: string): Promise<string[]> {
  let keys = repoKeysByCwd.get(cwd);
  if (!keys) {
    keys = invoke<{ stdout: string }>("run_git", { cwd, args: ["remote", "-v"] })
      .then((r) => [...new Set(r.stdout.split("\n").map((l) => repoKey(l.split(/\s+/)[1] ?? "")).filter((k): k is string => !!k))])
      .catch(() => []);
    repoKeysByCwd.set(cwd, keys);
    setTimeout(() => repoKeysByCwd.delete(cwd), 60_000);
  }
  return keys;
}
function openOutside(href: string) {
  if (isDesktop()) void shellOpen(href);
  else window.open(href, "_blank", "noopener");
}
function onChatLinkClick(e: MouseEvent) {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  const a = (e.target as Element | null)?.closest?.(".chat-messages a[href]") as HTMLAnchorElement | null;
  if (!a) return;
  const href = a.href;
  const local = localUrl(href);
  const pr = !local && prRef(href);
  if (!local && !pr) return;
  const wsId = Number(a.closest("[data-workspace-id]")?.getAttribute("data-workspace-id")) || ws.active?.id;
  const workspace = ws.workspaces.find((w) => w.id === wsId);
  if (!workspace) return;
  if (local) {
    // The phone has no Browser surface, and its localhost is not this machine.
    if (compact.value) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    ui.rightPanelVisible = true;
    rightPanelRef.value?.openBrowserUrl(local, workspace.id);
    return;
  }
  // Whose PR it is needs the remotes (async): take the click now, and hand a
  // foreign PR to the system browser ourselves.
  e.preventDefault();
  e.stopImmediatePropagation();
  void repoKeysFor(workspace.path).then((keys) => {
    const t = classifyChatLink(href, keys);
    if (t?.kind !== "pr") return openOutside(href);
    if (compact.value) void router.push("/project/pull-requests");
    else ui.rightPanelVisible = true;
    rightPanelRef.value?.openPullRequest(t.number, t.url, workspace.id);
  });
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
let unlistenMenuWhatsNew: UnlistenFn | null = null;
let unlistenMenuShortcuts: UnlistenFn | null = null;
let unlistenMenuNavigate: UnlistenFn | null = null;
let stopCoreLink: (() => void) | null = null;
let unlistenControl: (() => void) | undefined;
let unlistenWorkspacesChanged: UnlistenFn | null = null;
let unlistenWorktreesCleaned: UnlistenFn | null = null;

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
  hideSplash();
  void pendingReleaseNotes().then((n) => { whatsNew.value = n; }).catch(() => {});
  // Startup lands on the composer ("/"), never on the last session's thread —
  // reopening the app is a fresh start, and restoring a thread also left it
  // focused in the sidebar with nothing the user asked for behind it.
  if (route.path === "/" && ui.startupMode === "dashboard") void router.replace("/dashboard");
  playStartupChime();
  window.addEventListener("keydown", onKeydown);
  document.addEventListener("click", onLocalLinkClick, true);
  window.addEventListener("click", onChatLinkClick, true);
  window.addEventListener('mousemove', onResizeMove);
  window.addEventListener('mouseup', onResizeUp);
  setTimeout(() => update.check({ silent: true }), 3000);
  updateTimer = window.setInterval(() => update.check({ silent: true }), 6 * 60 * 60 * 1000);
  setTimeout(() => providers.probeAll().then(() => providers.checkLatest()), 3000);

  unlistenMenuUpdate = await listen("menu-check-update", () => {
    update.check({ silent: false });
  });
  unlistenMenuDocs = await listen("menu-open-docs", () => { docsOpen.value = true; });
  unlistenMenuWhatsNew = await listen("menu-whats-new", () => {
    void allReleaseNotes().then((n) => { whatsNew.value = n; });
  });
  unlistenMenuOnboarding = await listen("menu-show-onboarding", () => { onboardingOpen.value = true; });
  unlistenMenuShortcuts = await listen("menu-show-shortcuts", () => { cheatsheetOpen.value = true; });
  // The tray panel opened a thread/tab (desktop.go TrayOpen).
  unlistenMenuNavigate = await listen<string>("menu-navigate", (e) => { if (e.payload) void router.push(e.payload); });
  // The same from the external core's tray (plan 009), + its quit and Dock count.
  stopCoreLink = await startCoreLink((route) => void router.push(route));

  unlistenWorkspacesChanged = await listen("workspaces-changed", () => {
    ws.load();
  });
  // The hourly worktree cleanup (src-wails/worktreecleanup.go) or Settings'
  // "Clean up now" removed some worktrees: drop them and say so once.
  unlistenWorktreesCleaned = await listen<{ removed: { id: number; name: string }[] }>("worktrees-cleaned", (e) => {
    const removed = e.payload?.removed ?? [];
    if (!removed.length) return;
    removed.forEach((w) => ws.forgetWorktree(w.id));
    useNotificationsStore().push({
      type: "info",
      title: `Cleaned up ${removed.length} worktree${removed.length === 1 ? "" : "s"}`,
      body: removed.map((w) => w.name).join(", "),
      source: "system",
    });
  });

  // The control API's UI half: one app-wide listener performing the actions
  // agents ask for, and acking each with its result.
  unlistenControl = await installControlBridge();
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  document.removeEventListener("click", onLocalLinkClick, true);
  window.removeEventListener("click", onChatLinkClick, true);
  window.removeEventListener('mousemove', onResizeMove);
  window.removeEventListener('mouseup', onResizeUp);
  if (updateTimer) clearInterval(updateTimer);
  unlistenMenuUpdate?.();
  unlistenMenuDocs?.();
  unlistenMenuOnboarding?.();
  unlistenMenuWhatsNew?.();
  unlistenMenuShortcuts?.();
  unlistenMenuNavigate?.();
  stopCoreLink?.();
  unlistenWorkspacesChanged?.();
  unlistenWorktreesCleaned?.();
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
  rightPanel: () => { if (rpPopped.value) void openRightPanelWindow(); else ui.rightPanelVisible = !ui.rightPanelVisible; },
  // ⌘J: the Right Panel's terminal (the per-thread bottom terminal is gone).
  bottomPanel: toggleBottomTerminal,
  focusComposer,
  repaint: () => activeTerm()?.repaintAll(),
  newProject: () => openNewWorkspace(),
  pickProject: () => ui.pickProjectThenWelcome(),
  switchProvider: () => welcomeEl.value?.cycleProvider?.(),
  unread: jumpToUnread,
};

// ponytail: first visible composer in DOM order — the main pane's chat (or the
// welcome composer) precedes the Right Panel's sub-agent detail. A focused-pane
// registry would pick the right one in a split of two chats; add it if that bites.
function focusComposer() {
  const el = [...document.querySelectorAll<HTMLElement>(".composer-input")].find((c) => c.getClientRects().length);
  if (!el) return;
  el.focus();
  const sel = window.getSelection();
  if (el.isContentEditable && sel) { sel.selectAllChildren(el); sel.collapseToEnd(); }
}

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

