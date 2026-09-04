<template>
  <!-- data-tauri-drag-region makes the whole bar draggable with native decorations: true -->
  <div
    class="relative z-[100] flex h-[var(--titlebar-height)] shrink-0 items-center border-b border-border bg-panel [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)] [padding-top:env(titlebar-area-y,0px)]"
    :style="isDev ? { backgroundColor: 'color-mix(in srgb, var(--bg-panel), #dc2626 40%)' } : isBeta ? { backgroundColor: 'color-mix(in srgb, var(--bg-panel), #a21caf 40%)' } : undefined"
    data-tauri-drag-region
  >
    <!-- Spacer for native macOS traffic lights (~72px) -->
    <div class="w-[72px] shrink-0" data-tauri-drag-region />

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
    <div class="relative ml-1 flex shrink-0 [-webkit-app-region:no-drag]">
      <button
        class="notif-btn relative flex items-center rounded p-[5px] text-secondary-foreground [-webkit-app-region:no-drag] hover:bg-hover hover:text-foreground"
        :class="[notifOpen && 'text-accent', notifStore.unreadCount > 0 && 'text-success']"
        title="Notifications"
        @click.stop="toggleNotif"
      >
        <PhBell :size="14" />
        <span v-if="notifStore.unreadCount > 0" class="absolute right-px top-px flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-success px-[3px] text-[8px] font-bold leading-[14px] text-black pointer-events-none">
          {{ notifStore.unreadCount > 9 ? "9+" : notifStore.unreadCount }}
        </span>
      </button>
      <div v-if="notifOpen" class="tb-menu left-0 right-auto min-w-[280px] max-w-[320px] overflow-hidden p-0" @click.stop>
        <div class="flex items-center justify-between border-b border-border px-2.5 pb-1.5 pt-2">
          <span class="text-[11px] font-semibold text-foreground">Notifications</span>
          <button
            v-if="notifStore.history.length"
            class="rounded px-1 py-0.5 text-[10px] text-muted-foreground hover:bg-hover hover:text-secondary-foreground"
            @click="notifStore.clearHistory()"
          >Clear all</button>
        </div>
        <div v-if="!notifStore.history.length" class="px-3 py-5 text-center text-xs text-muted-foreground">No notifications</div>
        <div v-else class="max-h-[320px] overflow-y-auto p-1">
          <div
            v-for="item in notifStore.history"
            :key="item.id"
            class="notif-item flex items-start gap-2 rounded p-[7px_8px] hover:bg-hover"
            :class="[item.workspaceId && 'cursor-pointer']"
            @click="navigateToNotif(item.workspaceId, item.tabId)"
          >
            <PhCheckCircle v-if="item.type === 'done'" :size="13" class="mt-px shrink-0 text-success" />
            <PhWarning v-else-if="item.type === 'error'" :size="13" class="mt-px shrink-0 text-destructive" />
            <PhInfo v-else :size="13" class="mt-px shrink-0 text-accent" />
            <div class="min-w-0 flex-1">
              <div class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-medium text-foreground">{{ item.title }}</div>
              <div v-if="item.body" class="mt-px overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-secondary-foreground">{{ item.body }}</div>
            </div>
            <span class="mt-0.5 shrink-0 text-[9px] text-muted-foreground">{{ relTime(item.ts) }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="flex flex-1 items-center justify-center gap-1.5" data-tauri-drag-region>
      <button v-if="workspaceName" class="flex items-center rounded px-[5px] py-[3px] text-secondary-foreground [-webkit-app-region:no-drag] hover:bg-hover hover:text-foreground" @click="$emit('back')" title="Switch workspace">
        <PhHouse :size="13" />
      </button>
      <span class="font-mono text-[11px] text-secondary-foreground" data-tauri-drag-region>{{ workspaceName || "Burrow" }}</span>
      <CommitPushMenu v-if="branch" />
    </div>

    <div class="flex shrink-0 items-center gap-0.5 pr-2 [-webkit-app-region:no-drag]">
      <div class="relative flex gap-0">
        <button
          class="tb-btn rounded-l-[5px] rounded-r-none pr-1.5"
          :title="lastOpenTarget.id === 'finder' ? 'Reveal in Finder' : `Open in ${lastOpenTarget.name}`"
          :disabled="!folderPath"
          @click.stop="openIn(lastOpenTarget.id)"
        >
          <img v-if="lastOpenTarget.icon" :src="lastOpenTarget.icon" class="h-3.5 w-3.5 shrink-0" />
          <PhFolderOpen v-else :size="14" />
          <span class="text-[11px] font-medium">{{ lastOpenTarget.name }}</span>
        </button>
        <button
          class="tb-btn rounded-l-none rounded-r-[5px] border-l border-border pl-[5px] pr-[5px]"
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
      <div class="relative flex">
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
            <span class="font-mono text-[11px] text-foreground" :class="daemon && !daemon.connected && 'text-[#ff7676]'">
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
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhHouse, PhSidebarSimple, PhFolderOpen, PhCaretDown, PhFolderNotchOpen, PhGauge, PhCpu, PhMemory, PhStack, PhBroom, PhArrowsClockwise, PhBell, PhCheckCircle, PhWarning, PhInfo, PhSkull, PhCopy } from "@phosphor-icons/vue";
import { useNotificationsStore } from "@/stores/notifications";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import CommitPushMenu from "./CommitPushMenu.vue";

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

function navigateToNotif(workspaceId?: number, tabId?: number) {
  if (!workspaceId) return;
  const ws = wsStore.workspaces.find((w) => w.id === workspaceId);
  if (ws) {
    wsStore.open(ws);
    if (tabId != null) termTabs.activate(workspaceId, tabId);
  }
  notifOpen.value = false;
}

function toggleNotif() {
  notifOpen.value = !notifOpen.value;
  if (notifOpen.value) notifStore.markAllRead();
}

function relTime(ts: number): string {
  const diff = Date.now() - ts;
  if (diff < 60_000) return "now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
  return `${Math.floor(diff / 86_400_000)}d`;
}

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
});

const isDev = import.meta.env.DEV;
const isBeta = import.meta.env.VITE_APP_CHANNEL === "beta";
</script>

<style scoped>
.tb-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 4px 5px;
  border-radius: 4px;
  -webkit-app-region: no-drag;
}
.tb-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.tb-btn:disabled { opacity: 0.35; cursor: default; }
.tb-btn:disabled:hover { background: none; color: var(--text-secondary); }

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
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.5);
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
.tb-menu-item-danger:hover { background: rgba(220, 60, 60, 0.15); color: #ff7676; }
</style>
