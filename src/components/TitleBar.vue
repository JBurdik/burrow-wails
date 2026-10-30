<template>
  <!-- data-tauri-drag-region makes the whole bar draggable with native decorations: true -->
  <!-- Ghostex-style top row: no bar of its own. The left part is the top of
       the sidebar column (same width and fill), the rest belongs to the main
       area: navigation, what you're looking at, and the controls. -->
  <div
    class="titlebar relative z-[100] flex h-[var(--titlebar-height)] shrink-0 items-center [padding-top:env(titlebar-area-y,0px)]"
    data-tauri-drag-region
  >
    <div
      class="flex h-full shrink-0 items-center gap-0.5 pr-2"
      :class="sidebarVisible && 'relative after:absolute after:right-0 after:top-1/2 after:h-4 after:w-px after:-translate-y-1/2 after:bg-border'"
      :style="sidebarVisible ? { width: 'var(--sidebar-width)', background: 'var(--sidebar-bg)' } : undefined"
      data-tauri-drag-region
    >
    <!-- Spacer for native macOS traffic lights (~72px) -->
    <div class="tb-traffic w-[72px] shrink-0" data-tauri-drag-region />

    <!-- Sidebar toggle — the sidebar is hidden by default (⌘B) -->
    <button
      class="tb-btn shrink-0 [-webkit-app-region:no-drag]"
      :class="sidebarVisible && 'text-accent'"
      :title="sidebarVisible ? 'Hide sidebar (⌘B)' : 'Show sidebar (⌘B)'"
      @click="$emit('toggle-sidebar')"
    >
      <PhSidebarSimple :size="14" class="-scale-x-100" />
    </button>

    <!-- Notification center -->
    <div class="tb-wide relative ml-1 flex shrink-0 [-webkit-app-region:no-drag]">
      <button
        class="notif-btn tb-btn relative [-webkit-app-region:no-drag]"
        :class="[notifOpen && 'text-accent', bellColorClass, bellPulse && 'notif-bell-pulse']"
        title="Notifications"
        @click.stop="toggleNotif"
      >
        <PhBell :size="14" />
        <span v-if="notifStore.unreadCount > 0" class="absolute right-px top-px flex h-3.5 min-w-3.5 items-center justify-center rounded-full px-[3px] text-[8px] font-bold leading-[14px] text-black pointer-events-none" :class="badgeColorClass">
          {{ notifStore.unreadCount > 9 ? "9+" : notifStore.unreadCount }}
        </span>
      </button>
      <!-- Ghostex-style reading panel: header actions, UNREAD / READ card groups. -->
      <div v-if="notifOpen" class="tb-menu notif-panel left-0 right-auto flex max-h-[min(520px,calc(100vh-4rem))] w-[480px] max-w-[calc(100vw-2rem)] flex-col overflow-hidden p-0" @click.stop>
        <div class="flex h-9 shrink-0 items-stretch border-b border-border">
          <span class="flex items-center whitespace-nowrap pl-3 pr-10 text-[13px] font-bold text-foreground">Notifications</span>
          <button class="notif-action" :disabled="!notifStore.unreadCount" @click="jumpToNextUnread"><PhCaretRight :size="12" />Next unread</button>
          <button class="notif-action" :disabled="!notifStore.unreadCount" @click="notifStore.markAllRead()"><PhCheck :size="12" />Mark all read</button>
          <button class="notif-action rounded-tr-[5px]" :disabled="!notifStore.history.length" @click="notifStore.clearHistory()"><PhX :size="12" />Clear all</button>
        </div>
        <div v-if="notifStore.history.length" class="flex shrink-0 items-center gap-1 border-b border-border px-2.5 py-1.5">
          <button
            v-for="f in notifFilters"
            :key="f.id"
            class="rounded-[5px] px-2 py-0.5 text-[11px] font-medium"
            :class="notifFilter === f.id ? 'bg-hover text-foreground' : 'text-muted-foreground hover:bg-hover hover:text-foreground'"
            @click="notifFilter = f.id"
          >{{ f.label }}</button>
        </div>
        <div v-if="!notifStore.history.length" class="flex flex-col items-center gap-1.5 px-6 py-10 text-center">
          <PhBell :size="32" class="text-muted-foreground opacity-60" />
          <div class="mt-1 text-[13px] font-medium text-foreground">No notifications yet</div>
          <div class="text-[11.5px] text-muted-foreground">Agents that finish or need you will show up here.</div>
        </div>
        <div v-else-if="!notifGroups.length" class="px-3 py-8 text-center text-xs text-muted-foreground">Nothing here</div>
        <div v-else class="min-h-0 flex-1 overflow-y-auto p-2.5 pt-2">
          <template v-for="group in notifGroups" :key="group.label">
            <div class="px-0.5 pb-[7px] pt-1 text-[11px] font-bold text-muted-foreground" :class="group.label === 'READ' && notifGroups.length > 1 && 'mt-2.5'">{{ group.label }}</div>
            <div class="flex flex-col gap-[7px]">
              <div
                v-for="item in group.items"
                :key="item.id"
                class="notif-card group relative flex min-h-[72px] shrink-0 cursor-pointer items-start gap-2.5 rounded-md border p-2 pt-[9px]"
                :class="notifStore.isUnread(item) ? 'is-unread' : 'is-read'"
                :style="{ '--notif-accent': item.type === 'error' ? 'var(--color-destructive)' : 'var(--color-accent)' }"
                @click="openNotif(item)"
                @contextmenu.prevent="notifStore.setRead(item.id, notifStore.isUnread(item))"
              >
                <div class="notif-tile flex size-7 shrink-0 items-center justify-center rounded-md">
                  <component :is="kindIcon(item)" :size="16" />
                </div>
                <div class="flex min-w-0 flex-1 flex-col gap-[7px]">
                  <div class="flex h-[18px] min-w-0 items-center gap-2">
                    <span class="min-w-0 flex-1 truncate text-[13px] font-bold text-foreground">{{ item.title || "Session" }}<span v-if="item.count && item.count > 1" class="ml-1 font-medium text-muted-foreground">×{{ item.count }}</span></span>
                    <span class="shrink-0 text-[11px] leading-4 text-muted-foreground">{{ relTime(item.ts) }}</span>
                  </div>
                  <div v-if="item.body" class="line-clamp-2 text-[12px] font-medium leading-[16.2px] text-muted-foreground">{{ item.body }}</div>
                  <div class="notif-meta truncate pr-7 text-[11px] font-medium">{{ metaLine(item) }}</div>
                </div>
                <button
                  class="notif-check absolute bottom-2 right-2 hidden size-[22px] items-center justify-center rounded-[5px] border group-hover:flex"
                  :title="notifStore.isUnread(item) ? 'Mark read' : 'Dismiss'"
                  @click.stop="notifStore.isUnread(item) ? notifStore.setRead(item.id, true) : notifStore.remove(item.id)"
                ><PhCheck :size="14" /></button>
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>

      <button class="tb-btn [-webkit-app-region:no-drag]" title="Search (⌘P)" @click="ui.openSpotlight()"><PhMagnifyingGlass :size="15" /></button>
      <div class="flex-1" data-tauri-drag-region />
    </div>

    <!-- Main area: back · home · forward, then what's on screen. -->
    <div class="flex min-w-0 flex-1 items-center gap-1 pl-3" data-tauri-drag-region>
      <button class="tb-btn tb-wide [-webkit-app-region:no-drag]" title="Back" @click="router.back()"><PhCaretLeft :size="14" /></button>
      <button class="tb-btn tb-wide [-webkit-app-region:no-drag]" title="New thread" @click="ui.openWelcome()"><PhCircle :size="12" weight="bold" /></button>
      <button class="tb-btn tb-wide [-webkit-app-region:no-drag]" title="Forward" @click="router.forward()"><PhCaretRight :size="14" /></button>
      <span class="ml-2 min-w-0 truncate text-[13.5px] font-medium text-foreground" data-tauri-drag-region>{{ screenTitle }}</span>
      <span v-if="releaseChannel && ui.channelMarker !== 'off'" class="tb-wide shrink-0 rounded-[5px] border border-border px-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground" data-tauri-drag-region>{{ releaseChannel }}</span>
      <div class="min-w-4 flex-1" data-tauri-drag-region />
      <CommitPushMenu v-if="branch" class="tb-wide [-webkit-app-region:no-drag]" />
    </div>

    <div class="flex shrink-0 items-center gap-0.5 pr-2 [-webkit-app-region:no-drag]">
      <!-- Opens an app on the Mac: no use to a browser client. -->
      <div class="tb-web-hide relative flex gap-0">
        <button
          class="tb-btn rounded-l-[var(--radius-nav)] rounded-r-none pr-1.5"
          :title="lastOpenTarget.id === 'finder' ? 'Reveal in Finder' : `Open in ${lastOpenTarget.name}`"
          :disabled="!folderPath"
          @click.stop="openIn(lastOpenTarget.id)"
        >
          <img v-if="lastOpenTarget.icon" :src="lastOpenTarget.icon" class="h-3.5 w-3.5 shrink-0" />
          <PhFolderOpen v-else :size="14" />
          <span class="text-[11px] font-medium">{{ lastOpenTarget.name }}</span>
        </button>
        <button
          class="tb-btn rounded-l-none rounded-r-[var(--radius-nav)] border-l border-border pl-[5px] pr-[5px]"
          title="Open folder in…"
          :disabled="!folderPath"
          @click.stop="toggleMenu"
        >
          <PhCaretDown :size="9" />
        </button>
        <div v-if="menuOpen" class="tb-menu" @click.stop>
          <button
            v-for="t in openTargets"
            :key="t.id"
            class="tb-menu-item"
            :class="lastOpenIn === t.id && '!text-accent'"
            @click="openIn(t.id)"
          >
            <img v-if="t.icon" :src="t.icon" class="h-3.5 w-3.5 shrink-0" />
            <PhFolderNotchOpen v-else :size="14" />
            {{ t.id === "finder" ? "Reveal in Finder" : `Open in ${t.name}` }}
          </button>
          <div class="my-1 h-px bg-border" />
          <button class="tb-menu-item" @click="copyPath()"><PhCopy :size="14" />Copy Path</button>
        </div>
      </div>
      <div class="tb-wide relative flex">
        <button
          class="tb-btn"
          title="System & daemon stats"
          @click.stop="toggleStats"
        >
          <PhGauge :size="14" />
          <PhCaretDown :size="9" />
        </button>
        <div v-if="statsOpen" class="tb-menu min-w-[200px] p-2" @click.stop>
          <div class="flex items-center justify-between px-0.5 py-0.5 font-sans text-xs text-secondary-foreground">
            <span class="flex items-center gap-1.5"><PhCpu :size="13" />CPU</span>
            <span class="font-mono text-[11px] text-foreground">{{ stats ? stats.cpu_percent.toFixed(0) + "%" : "…" }}</span>
          </div>
          <div class="my-[3px] mx-0.5 mb-2 h-1 overflow-hidden rounded-sm bg-hover"><div class="h-full rounded-sm bg-accent transition-[width] duration-[400ms]" :style="{ width: (stats?.cpu_percent ?? 0) + '%' }" /></div>

          <div class="flex items-center justify-between px-0.5 py-0.5 font-sans text-xs text-secondary-foreground">
            <span class="flex items-center gap-1.5"><PhMemory :size="13" />RAM</span>
            <span class="font-mono text-[11px] text-foreground">{{ memText }}</span>
          </div>
          <div class="my-[3px] mx-0.5 mb-2 h-1 overflow-hidden rounded-sm bg-hover"><div class="h-full rounded-sm bg-accent transition-[width] duration-[400ms]" :style="{ width: memPct + '%' }" /></div>

          <div class="my-1.5 h-px bg-border" />

          <div class="flex items-center justify-between px-0.5 py-0.5 font-sans text-xs text-secondary-foreground">
            <span class="flex items-center gap-1.5"><PhStack :size="13" />Daemon</span>
            <span class="font-mono text-[11px] text-foreground" :class="daemon && !daemon.connected && 'text-destructive'">
              {{ daemon ? (daemon.connected ? daemon.alive + "/" + daemon.total + " live" : "offline") : "…" }}
            </span>
          </div>
          <div v-if="daemon?.pid" class="px-0.5 pb-0.5 font-mono text-[10px] text-muted-foreground">pid {{ daemon.pid }}</div>

          <div class="my-1.5 h-px bg-border" />

          <button class="tb-menu-item" :disabled="busy" @click="cleanDaemon">
            <PhBroom :size="14" />Clean dead sessions
          </button>
          <button
            class="tb-menu-item"
            :disabled="busy"
            title="Kill alive PTYs that no open or saved tab references (closed-tab leftovers)"
            @click="killOrphans"
          >
            <PhSkull :size="14" />Kill orphaned sessions
          </button>
          <button class="tb-menu-item tb-menu-item-danger" :disabled="busy" @click="restartDaemon">
            <PhArrowsClockwise :size="14" />Restart daemon
          </button>
          <div v-if="actionMsg" class="px-0.5 pb-0.5 pt-1.5 text-center font-sans text-[11px] text-muted-foreground">{{ actionMsg }}</div>
        </div>
      </div>
      <button
        class="tb-btn"
        :class="rightPanelVisible && 'text-accent'"
        :title="rightPanelVisible ? 'Hide explorer & git' : 'Show explorer & git'"
        @click="$emit('toggle-rightpanel')"
      >
        <PhSidebarSimple :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhMagnifyingGlass, PhCaretLeft, PhCaretRight, PhCircle, PhSidebarSimple, PhFolderOpen, PhCaretDown, PhFolderNotchOpen, PhGauge, PhCpu, PhMemory, PhStack, PhBroom, PhArrowsClockwise, PhBell, PhCheck, PhX, PhWarning, PhInfo, PhSkull, PhCopy, PhRobot, PhGitBranch } from "@phosphor-icons/vue";
import { useNotificationsStore, type HistoryItem, type NotifSource } from "@/stores/notifications";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import CommitPushMenu from "./CommitPushMenu.vue";
import { useUIStore } from "@/stores/ui";
import { releaseChannel } from "@/lib/releaseChannel";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { router } from "@/router";

const props = defineProps<{ workspaceName?: string; branch?: string; folderPath?: string; rightPanelVisible?: boolean; sidebarVisible?: boolean }>();
defineEmits(["back", "toggle-rightpanel", "toggle-sidebar"]);

const menuOpen = ref(false);
type OpenTarget = { id: string; name: string; icon: string };
const FINDER_TARGET: OpenTarget = { id: "finder", name: "Finder", icon: "" };

// "Open in…" targets are the installed apps Go finds in /Applications
// (see ListOpenTargets in fs.go), fetched once and cached for the session.
const openTargets = ref<OpenTarget[]>([FINDER_TARGET]);
let openTargetsLoaded = false;
async function loadOpenTargets() {
  if (openTargetsLoaded) return;
  openTargetsLoaded = true;
  try {
    openTargets.value = await invoke<OpenTarget[]>("list_open_targets", {});
  } catch (e) {
    console.error("list_open_targets failed", e);
  }
}

// Last-used target is remembered per project (keyed by folder path), not globally.
const LAST_OPEN_IN_LEGACY_KEY = "tb-last-open-in";
const LAST_OPEN_IN_CONFIG_KEY = "titlebarLastOpenInByProject";
const lastOpenInByProject = ref<Record<string, string>>({});
configReady.then(() => {
  migrateFromLocalStorage(LAST_OPEN_IN_LEGACY_KEY, LAST_OPEN_IN_CONFIG_KEY);
  const stored = getConfig<Record<string, string> | string>(LAST_OPEN_IN_CONFIG_KEY, {});
  lastOpenInByProject.value = typeof stored === "string" ? {} : stored;
});
const lastOpenIn = computed(() => (props.folderPath && lastOpenInByProject.value[props.folderPath]) || "finder");
const lastOpenTarget = computed(() => openTargets.value.find((t) => t.id === lastOpenIn.value) ?? FINDER_TARGET);

function toggleMenu() {
  menuOpen.value = !menuOpen.value;
  if (menuOpen.value) loadOpenTargets();
}

// ── Notification center ─────────────────────────────────────────────────────
const notifStore = useNotificationsStore();
const notifOpen = ref(false);
const wsStore = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const chats = useClaudeChatsStore();

/** What the main area shows: the open thread or tab, else the composer or project. */
const screenTitle = computed(() => {
  if (ui.welcomeVisible || !ui.viewingTabs) return props.workspaceName ? `New thread · ${props.workspaceName}` : "Burrow";
  const ws = wsStore.active;
  if (!ws) return "Burrow";
  const tab = termTabs.tabsByWs[ws.id]?.find((t) => t.id === termTabs.activeByWs[ws.id]);
  if (!tab) return ws.name;
  return (tab.chatId != null ? chats.sessions.find((c) => c.id === tab.chatId)?.title : undefined) ?? tab.title;
});

function openNotif(item: HistoryItem) {
  notifStore.setRead(item.id, true);
  if (!item.workspaceId) return;
  const ws = wsStore.workspaces.find((w) => w.id === item.workspaceId);
  if (ws) {
    wsStore.open(ws);
    if (item.tabId != null) termTabs.activate(item.workspaceId, item.tabId);
  }
  notifOpen.value = false;
}

/** Newest unread row with somewhere to go; history is newest-first. */
function jumpToNextUnread() {
  const item = notifStore.history.find((i) => notifStore.isUnread(i) && i.workspaceId) ?? notifStore.history.find(notifStore.isUnread);
  if (item) openNotif(item);
  notifOpen.value = false;
}

function toggleNotif() {
  notifOpen.value = !notifOpen.value;
}

function relTime(ts: number): string {
  const diff = Date.now() - ts;
  if (diff < 60_000) return "now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
  return `${Math.floor(diff / 86_400_000)}d`;
}

const KIND_LABEL: Record<HistoryItem["type"], string> = { done: "Finished", error: "Failed", info: "Notification", pending: "Running" };
const SOURCE_ICON = { agent: PhRobot, git: PhGitBranch, system: PhInfo };

function kindIcon(item: HistoryItem) {
  if (item.type === "error") return PhWarning;
  return SOURCE_ICON[item.source ?? "system"];
}

/** "Project · Tab · Finished", the kind alone when the row has no workspace. */
function metaLine(item: HistoryItem): string {
  const ws = item.workspaceId ? wsStore.workspaces.find((w) => w.id === item.workspaceId)?.name : undefined;
  const tab = ws && item.tabId != null ? termTabs.tabsByWs[item.workspaceId!]?.find((t) => t.id === item.tabId)?.title : undefined;
  return [ws, tab, KIND_LABEL[item.type]].filter(Boolean).join(" · ");
}

const notifFilters: { id: "all" | NotifSource; label: string }[] = [
  { id: "all", label: "All" },
  { id: "agent", label: "Agents" },
  { id: "git", label: "Git" },
];
const notifFilter = ref<"all" | NotifSource>("all");

const notifGroups = computed(() => {
  const shown = notifFilter.value === "all"
    ? notifStore.history
    : notifStore.history.filter((i) => (i.source ?? "system") === notifFilter.value);
  const unread = shown.filter(notifStore.isUnread);
  const read = shown.filter((i) => !notifStore.isUnread(i));
  return [
    { label: "UNREAD", items: unread },
    { label: "READ", items: read },
  ].filter((g) => g.items.length > 0);
});

const bellColorClass = computed(() => {
  switch (notifStore.worstUnreadType) {
    case "error": return "text-destructive";
    case "done": return "text-success";
    case "info": return "text-accent";
    default: return "";
  }
});
const badgeColorClass = computed(() => {
  switch (notifStore.worstUnreadType) {
    case "error": return "bg-destructive";
    case "info": return "bg-accent";
    default: return "bg-success";
  }
});

// One-shot pulse when unreadCount rises, e.g. a new notification arrives while the panel is closed.
const bellPulse = ref(false);
let bellPulseTimer: number | undefined;
watch(
  () => notifStore.unreadCount,
  (count, prev) => {
    if (count <= prev) return;
    bellPulse.value = false;
    requestAnimationFrame(() => {
      bellPulse.value = true;
      clearTimeout(bellPulseTimer);
      bellPulseTimer = window.setTimeout(() => (bellPulse.value = false), 300);
    });
  }
);

// ── Stats dropdown ──────────────────────────────────────────────────────────
type SystemStats = { cpu_percent: number; mem_used: number; mem_total: number };
type DaemonStats = { connected: boolean; pid: number | null; total: number; alive: number };

const statsOpen = ref(false);
const stats = ref<SystemStats | null>(null);
const daemon = ref<DaemonStats | null>(null);
const busy = ref(false);
const actionMsg = ref("");
let statsTimer: number | undefined;

const memPct = computed(() =>
  stats.value && stats.value.mem_total ? (stats.value.mem_used / stats.value.mem_total) * 100 : 0
);
const memText = computed(() => {
  if (!stats.value) return "…";
  const gb = (b: number) => (b / 1024 ** 3).toFixed(1);
  return `${gb(stats.value.mem_used)} / ${gb(stats.value.mem_total)} GB`;
});

async function refreshStats() {
  try {
    [stats.value, daemon.value] = await Promise.all([
      invoke<SystemStats>("system_stats"),
      invoke<DaemonStats>("daemon_stats"),
    ]);
  } catch (e) {
    console.error("stats refresh failed", e);
  }
}

function toggleStats() {
  statsOpen.value = !statsOpen.value;
  if (statsOpen.value) {
    actionMsg.value = "";
    refreshStats();
    statsTimer = window.setInterval(refreshStats, 2000);
  } else {
    clearInterval(statsTimer);
  }
}

async function cleanDaemon() {
  busy.value = true;
  try {
    const n = await invoke<number>("clean_daemon");
    actionMsg.value = n ? `Reaped ${n} dead session${n === 1 ? "" : "s"}` : "No dead sessions";
    await refreshStats();
  } catch (e) {
    actionMsg.value = "Clean failed";
    console.error(e);
  } finally {
    busy.value = false;
  }
}

async function killOrphans() {
  busy.value = true;
  try {
    // Every pty id the UI currently knows is live (across all opened workspaces).
    // Rust unions this with the saved terminal_tabs rows so reattachable / not-yet-
    // opened sessions are never killed — only true closed-tab leftovers.
    const keepIds = Object.values(termTabs.tabsByWs).flat().map((t) => t.id);
    const n = await invoke<number>("kill_orphan_sessions", { keepIds });
    actionMsg.value = n ? `Killed ${n} orphaned session${n === 1 ? "" : "s"}` : "No orphaned sessions";
    await refreshStats();
  } catch (e) {
    actionMsg.value = "Kill failed";
    console.error(e);
  } finally {
    busy.value = false;
  }
}

async function restartDaemon() {
  busy.value = true;
  actionMsg.value = "Restarting…";
  try {
    const pid = await invoke<number>("restart_daemon");
    actionMsg.value = `Daemon restarted (pid ${pid})`;
    await refreshStats();
  } catch (e) {
    actionMsg.value = "Restart failed";
    console.error(e);
  } finally {
    busy.value = false;
  }
}

async function openIn(target: string) {
  menuOpen.value = false;
  if (!props.folderPath) return;
  lastOpenInByProject.value = { ...lastOpenInByProject.value, [props.folderPath]: target };
  setConfig(LAST_OPEN_IN_CONFIG_KEY, lastOpenInByProject.value);
  try {
    await invoke("open_path_in", { path: props.folderPath, target });
  } catch (e) {
    console.error("open_path_in failed", e);
  }
}

async function copyPath() {
  menuOpen.value = false;
  if (!props.folderPath) return;
  await navigator.clipboard.writeText(props.folderPath);
}

function onDocClick() {
  menuOpen.value = false;
  notifOpen.value = false;
  if (statsOpen.value) { statsOpen.value = false; clearInterval(statsTimer); }
}
onMounted(() => {
  window.addEventListener("click", onDocClick);
  loadOpenTargets();
});
onBeforeUnmount(() => {
  window.removeEventListener("click", onDocClick);
  clearInterval(statsTimer);
  clearTimeout(bellPulseTimer);
});

</script>

<style scoped>
.tb-btn {
  background: transparent;
  border: 1px solid transparent;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 5px 6px;
  border-radius: 7px;
  -webkit-app-region: no-drag;
}
.tb-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.tb-btn:disabled { opacity: 0.35; cursor: default; }
.tb-btn:disabled:hover { background: var(--surface); color: var(--text-secondary); }

.tb-menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  min-width: 168px;
  background: var(--bg-dropdown, var(--bg-panel));
  backdrop-filter: var(--blur-dropdown, blur(18px)) saturate(140%);
  -webkit-backdrop-filter: var(--blur-dropdown, blur(18px)) saturate(140%);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px;
  z-index: 1000;
}

.tb-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-family: var(--font-ui);
  font-size: 12px;
  text-align: left;
  padding: 6px 8px;
  border-radius: 4px;
  white-space: nowrap;
}
.tb-menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.tb-menu-item:disabled { opacity: 0.4; cursor: default; }
.tb-menu-item:disabled:hover { background: none; color: var(--text-secondary); }
.tb-menu-item-danger:hover { background: color-mix(in srgb, var(--color-destructive) 15%, transparent); color: var(--color-destructive); }

.notif-action {
  display: flex;
  flex: 1 1 0;
  min-width: 0;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 4px;
  border-left: 1px solid var(--border);
  white-space: nowrap;
  font-size: 11.5px;
  color: color-mix(in srgb, var(--text-primary) 78%, transparent);
}
.notif-action:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
.notif-action:disabled { opacity: 0.4; cursor: default; }

/* Cards: accent-tinted while unread (red for errors), neutral and dimmed once read. */
.notif-card.is-unread {
  border-color: color-mix(in srgb, var(--notif-accent) 30%, transparent);
  background: color-mix(in srgb, var(--notif-accent) 5%, transparent);
}
.notif-card.is-unread:hover {
  border-color: color-mix(in srgb, var(--notif-accent) 48%, transparent);
  background: color-mix(in srgb, var(--notif-accent) 10%, transparent);
}
.notif-card.is-read {
  opacity: 0.72;
  border-color: color-mix(in srgb, var(--text-primary) 10%, transparent);
  background: color-mix(in srgb, var(--text-primary) 2.5%, transparent);
}
.notif-card.is-read:hover {
  opacity: 1;
  border-color: color-mix(in srgb, var(--text-primary) 18%, transparent);
  background: color-mix(in srgb, var(--text-primary) 5%, transparent);
}
.notif-tile { color: var(--text-secondary); background: color-mix(in srgb, var(--text-primary) 10%, transparent); }
.is-unread .notif-tile { color: var(--notif-accent); background: color-mix(in srgb, var(--notif-accent) 16%, transparent); }
.notif-meta { color: var(--text-muted); }
.is-unread .notif-meta { color: color-mix(in srgb, var(--notif-accent) 85%, var(--text-primary)); }
.notif-check {
  color: var(--text-primary);
  border-color: color-mix(in srgb, var(--text-primary) 16%, transparent);
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
}
.notif-check:hover { background: color-mix(in srgb, var(--text-primary) 22%, transparent); }
.is-unread .notif-check {
  border-color: color-mix(in srgb, var(--notif-accent) 45%, transparent);
  background: color-mix(in srgb, var(--notif-accent) 18%, transparent);
}
.is-unread .notif-check:hover { background: color-mix(in srgb, var(--notif-accent) 32%, transparent); }

@keyframes notif-bell-pulse {
  0% { transform: scale(1); }
  40% { transform: scale(1.35); }
  100% { transform: scale(1); }
}
.notif-bell-pulse :deep(svg) {
  animation: notif-bell-pulse 300ms ease-out;
}
</style>
