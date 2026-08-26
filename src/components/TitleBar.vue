<template>
  <!-- data-tauri-drag-region makes the whole bar draggable with native decorations: true -->
  <div
    class="relative z-[100] flex h-[var(--titlebar-height)] shrink-0 items-center border-b border-border bg-panel [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)] [padding-top:env(titlebar-area-y,0px)]"
    :class="isDev && 'bg-[#5c1a1a]'"
    data-tauri-drag-region
  >
    <!-- Spacer for native macOS traffic lights (~72px) -->
    <div class="w-[72px] shrink-0" data-tauri-drag-region />

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

    <!-- Claude plan-usage strip — real utilization %, same data claude.ai shows.
         One bar per limit window (5h session, weekly, weekly-Sonnet). -->
    <!-- Profile selector: always visible when multiple profiles exist -->
    <div v-if="profilesStore.profiles.length > 1" class="relative mr-1 flex shrink-0 [-webkit-app-region:no-drag]" data-tauri-drag-region>
      <button
        class="flex items-center gap-[3px] rounded border-0 bg-transparent px-1 py-px font-sans text-[9px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
        :class="usageProfileId !== DEFAULT_PROFILE_ID && '!text-accent'"
        :title="`Showing usage for: ${usageProfile?.name ?? 'Default'}`"
        @click.stop="usageProfileMenuOpen = !usageProfileMenuOpen"
      >
        <PhUserGear :size="10" />
        <span class="max-w-[60px] overflow-hidden text-ellipsis whitespace-nowrap">{{ usageProfile?.name ?? 'Default' }}</span>
        <PhCaretDown :size="7" weight="bold" />
      </button>
      <div v-if="usageProfileMenuOpen" class="tb-menu left-0 top-[calc(100%+4px)] min-w-[140px]" @click.stop>
        <button
          v-for="p in profilesStore.profiles"
          :key="p.id"
          class="tb-menu-item"
          :class="usageProfileId === p.id && '!text-accent'"
          @click="selectUsageProfile(p.id)"
        >
          <PhUserGear :size="12" />
          {{ p.name }}
        </button>
      </div>
    </div>
    <div
      v-if="usageBars.length || usageError"
      class="ml-1 flex shrink-0 items-center gap-2.5 rounded-md border border-border bg-hover px-2.5 py-[3px] [-webkit-app-region:no-drag]"
      :class="usageError && 'opacity-50'"
      :title="usageError ? `usage unavailable: ${usageError}` : 'claude plan usage'"
      data-tauri-drag-region
    >
      <ClaudeIcon :size="11" class="shrink-0 text-amber-600" style="color:#d97706" />
      <span
        v-for="b in usageBars"
        :key="b.key"
        class="usage-bar inline-flex items-center gap-[5px] font-mono text-[10px] text-secondary-foreground"
        :class="[usageSeverity(b.pct) && `usage-bar-${usageSeverity(b.pct)}`, b.credit ? 'usage-bar-credit' : '']"
        :title="usageBarTitle(b)"
      >
        <span class="ub-label uppercase tracking-[0.5px] text-muted-foreground">{{ b.label }}</span>
        <template v-if="!b.local">
          <span class="ub-track inline-block h-1 w-11 overflow-hidden rounded-[3px] border border-border bg-white/[0.08]"><span class="ub-fill block h-full w-0 bg-success transition-[width,background] duration-[400ms,200ms]" :style="{ width: Math.min(b.pct, 100) + '%' }" /></span>
          <span v-if="!b.credit" class="min-w-[26px] text-right [font-variant-numeric:tabular-nums]">{{ b.pct }}%</span>
        </template>
      </span>
    </div>
    <!-- Stale-login hint: token expired, we don't refresh (Claude CLI does on
         launch). Tells the user to run claude in this profile to get live %. -->
    <div
      v-if="usageStale"
      class="ml-1.5 inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded bg-hover px-1.5 py-px font-sans text-[9px] text-muted-foreground"
      :title="`${usageProfile?.name ?? 'Profile'} logged out — run ${usageProfile?.command || 'claude'} to refresh usage`"
      data-tauri-drag-region
    >
      <PhSignOut :size="11" />
      <span>run <code class="font-mono text-secondary-foreground">{{ usageProfile?.command || 'claude' }}</code></span>
    </div>

    <div class="flex flex-1 items-center justify-center gap-1.5" data-tauri-drag-region>
      <button v-if="workspaceName" class="flex items-center rounded px-[5px] py-[3px] text-secondary-foreground [-webkit-app-region:no-drag] hover:bg-hover hover:text-foreground" @click="$emit('back')" title="Switch workspace">
        <PhHouse :size="13" />
      </button>
      <span class="font-mono text-[11px] text-secondary-foreground" data-tauri-drag-region>{{ workspaceName || "Burrow" }}</span>
      <div v-if="branch" class="relative [-webkit-app-region:no-drag]">
        <button
          class="flex items-center gap-[3px] rounded-md border border-border/70 bg-transparent px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground transition-colors hover:border-border hover:bg-hover hover:text-secondary-foreground"
          :title="`Branch: ${branch} — click to switch`"
          @click.stop="openBranchPicker"
        >
          <PhGitBranch :size="11" />
          {{ branch }}
        </button>
        <div v-if="branchPickerOpen" class="absolute left-1/2 top-[calc(100%+5px)] z-[2000] w-[220px] -translate-x-1/2 overflow-hidden rounded-md border border-border bg-panel shadow-[0_8px_24px_rgba(0,0,0,0.45)]" @click.stop>
          <input
            ref="branchInputEl"
            v-model="branchFilter"
            class="box-border w-full border-0 border-b border-border bg-transparent px-[9px] py-[7px] font-mono text-[11px] text-foreground outline-none placeholder:text-muted-foreground"
            placeholder="Switch or create branch…"
            @keydown.enter="onBranchEnter"
            @keydown.esc="branchPickerOpen = false"
          />
          <div class="max-h-[180px] overflow-y-auto">
            <div v-if="branchLoading" class="flex items-center gap-1.5 px-[9px] py-[5px] font-mono text-[11px] italic text-muted-foreground">Loading…</div>
            <template v-else>
              <div
                v-for="b in filteredBranches"
                :key="b"
                class="flex cursor-pointer items-center gap-1.5 px-[9px] py-[5px] font-mono text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground"
                :class="b === branch && 'text-accent'"
                @click="switchBranch(b)"
              >
                <PhGitBranch :size="10" />
                <span>{{ b }}</span>
                <span v-if="b === branch" class="ml-auto not-italic text-accent">✓</span>
              </div>
              <div
                v-if="showCreateOption"
                class="flex cursor-pointer items-center gap-1.5 px-[9px] py-[5px] font-mono text-[11px] italic text-muted-foreground hover:bg-hover hover:text-foreground"
                @click="createBranch(branchFilter.trim())"
              >
                <PhPlus :size="10" />
                <span>Create "{{ branchFilter.trim() }}"</span>
              </div>
              <div v-if="!branchLoading && filteredBranches.length === 0 && !showCreateOption" class="px-2.5 py-2.5 text-center text-[10px] text-muted-foreground">
                No branches found
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <div class="flex shrink-0 items-center gap-0.5 pr-2 [-webkit-app-region:no-drag]">
      <div class="relative flex gap-0">
        <button
          class="tb-btn rounded-l-[5px] rounded-r-none pr-1.5"
          :title="OPEN_IN_META[lastOpenIn].title"
          :disabled="!folderPath"
          @click.stop="openIn(lastOpenIn)"
        >
          <PhFolderOpen :size="14" />
          <span class="text-[11px] font-medium">{{ OPEN_IN_META[lastOpenIn].label }}</span>
        </button>
        <button
          class="tb-btn rounded-l-none rounded-r-[5px] border-l border-border pl-[5px] pr-[5px]"
          title="Open folder in…"
          :disabled="!folderPath"
          @click.stop="menuOpen = !menuOpen"
        >
          <PhCaretDown :size="9" />
        </button>
        <div v-if="menuOpen" class="tb-menu" @click.stop>
          <button class="tb-menu-item" :class="lastOpenIn === 'finder' && '!text-accent'" @click="openIn('finder')"><PhFolderNotchOpen :size="14" />Reveal in Finder</button>
          <button class="tb-menu-item" :class="lastOpenIn === 'vscode' && '!text-accent'" @click="openIn('vscode')"><PhCode :size="14" />Open in VS Code</button>
          <button class="tb-menu-item" :class="lastOpenIn === 'zed' && '!text-accent'" @click="openIn('zed')"><PhLightning :size="14" />Open in Zed</button>
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
      <button class="tb-btn" title="Settings (⌘,)" @click="$emit('open-settings')">
        <PhGear :size="14" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhHouse, PhGitBranch, PhSidebarSimple, PhFolderOpen, PhGear, PhCaretDown, PhFolderNotchOpen, PhCode, PhLightning, PhGauge, PhCpu, PhMemory, PhStack, PhBroom, PhArrowsClockwise, PhBell, PhCheckCircle, PhWarning, PhInfo, PhPlus, PhSkull, PhUserGear, PhSignOut, PhCopy } from "@phosphor-icons/vue";
import { useNotificationsStore } from "@/stores/notifications";
import { useWorkspaceStore } from "@/stores/workspace";
import { useProfilesStore, DEFAULT_PROFILE_ID } from "@/stores/profiles";
import { useGitStore } from "@/stores/git";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

const props = defineProps<{ workspaceName?: string; branch?: string; folderPath?: string; rightPanelVisible?: boolean }>();
defineEmits(["back", "toggle-rightpanel", "open-settings"]);

const menuOpen = ref(false);
type OpenInTarget = "finder" | "vscode" | "zed";
const LAST_OPEN_IN_LEGACY_KEY = "tb-last-open-in";
const LAST_OPEN_IN_CONFIG_KEY = "titlebarLastOpenIn";
const lastOpenIn = ref<OpenInTarget>("finder");
configReady.then(() => {
  migrateFromLocalStorage(LAST_OPEN_IN_LEGACY_KEY, LAST_OPEN_IN_CONFIG_KEY);
  lastOpenIn.value = getConfig<OpenInTarget>(LAST_OPEN_IN_CONFIG_KEY, "finder");
});
const OPEN_IN_META: Record<OpenInTarget, { title: string; label: string }> = {
  finder: { title: "Reveal in Finder", label: "Finder" },
  vscode:  { title: "Open in VS Code", label: "VS Code" },
  zed:     { title: "Open in Zed",     label: "Zed" },
};

// ── Branch picker ───────────────────────────────────────────────────────────
const git = useGitStore();
const branchPickerOpen = ref(false);
const branchFilter = ref("");
const branchLoading = ref(false);
const branchInputEl = ref<HTMLInputElement | null>(null);

const filteredBranches = computed(() => {
  const q = branchFilter.value.toLowerCase();
  return q ? git.branches.filter(b => b.toLowerCase().includes(q)) : git.branches;
});
const showCreateOption = computed(() => {
  const q = branchFilter.value.trim();
  return q && !git.branches.includes(q);
});

async function openBranchPicker() {
  if (branchPickerOpen.value) { branchPickerOpen.value = false; return; }
  if (!props.folderPath) return;
  branchPickerOpen.value = true;
  branchFilter.value = "";
  branchLoading.value = true;
  try {
    await git.fetchBranches();
  } finally {
    branchLoading.value = false;
    await nextTick();
    branchInputEl.value?.focus();
  }
}

async function switchBranch(name: string) {
  branchPickerOpen.value = false;
  try { await git.switchBranch(name); }
  catch (e) { console.error("branch switch failed", e); }
}

async function createBranch(name: string) {
  if (!name) return;
  branchPickerOpen.value = false;
  try { await git.createBranch(name); }
  catch (e) { console.error("branch create failed", e); }
}

function onBranchEnter() {
  if (filteredBranches.value.length === 1) { switchBranch(filteredBranches.value[0]); return; }
  if (showCreateOption.value) createBranch(branchFilter.value.trim());
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

// ── Claude plan-usage strip ──────────────────────────────────────────────────
// Real utilization % from the OAuth usage endpoint (Rust `claude_plan_usage`),
// the same numbers claude.ai's UI shows. Polled every 60s; Rust caches 60s.
// Fallback for org/team accounts (rate_limits_available=false) or missing creds:
// read local JSONL transcripts via `claude_usage_5h`, show raw token count.
type UsageWindow = { utilization: number; resets_at?: string };
type ExtraUsage = { is_enabled: boolean; monthly_limit?: number; used_credits?: number; utilization?: number };
type PlanUsage = Record<string, UsageWindow | undefined> & { extra_usage?: ExtraUsage };
type UsageBar = { key: string; label: string; pct: number; resets?: string; local?: boolean; credit?: boolean };

// ── Usage profile selector ──────────────────────────────────────────────────
const profilesStore = useProfilesStore();
const USAGE_PROFILE_LEGACY_KEY = "burrow.titlebar.usageProfileId";
const USAGE_PROFILE_CONFIG_KEY = "titlebarUsageProfileId";
const usageProfileId = ref<string>(DEFAULT_PROFILE_ID);
configReady.then(() => {
  migrateFromLocalStorage(USAGE_PROFILE_LEGACY_KEY, USAGE_PROFILE_CONFIG_KEY);
  usageProfileId.value = getConfig<string>(USAGE_PROFILE_CONFIG_KEY, DEFAULT_PROFILE_ID);
});
const usageProfile = computed(() => profilesStore.get(usageProfileId.value));
const usageProfileMenuOpen = ref(false);
function selectUsageProfile(id: string) {
  usageProfileId.value = id;
  setConfig(USAGE_PROFILE_CONFIG_KEY, id);
  usageProfileMenuOpen.value = false;
  refreshUsage();
}

const planUsage = ref<PlanUsage | null>(null);
const localUsage = ref<{ outputTokens: number; turnCount: number } | null>(null);
const usageError = ref<string | null>(null);
// Token exists but expired: profile is "logged out". We don't refresh (Claude CLI
// does on launch), so hint the user to run claude rather than show a stale/empty bar.
const usageStale = ref(false);
let usageTimer: number | undefined;

// Errors that mean the OAuth usage API won't work for this account type —
// fall back to local transcript scan instead of showing an error.
const LOCAL_FALLBACK_ERRORS = new Set(["token_expired", "no_credentials", "permission_error"]);

async function refreshUsage(force = false) {
  const profile = usageProfile.value;
  const cd = profile?.configDir;
  const args: Record<string, unknown> = {};
  if (cd) args.configDir = cd;
  if (force) args.force = true;
  const prevError = usageError.value;

  // Org/team accounts can't use the OAuth usage API — go straight to local JSONL scan.
  if (profile?.orgAccount) {
    planUsage.value = null;
    usageError.value = null;
    usageStale.value = false;
    try {
      const local = await invoke<{ outputTokens: number; turnCount: number }>(
        "claude_usage_5h",
        cd ? { configDir: cd } : {},
      );
      localUsage.value = local;
    } catch (e) {
      usageError.value = "invoke_failed";
      if (prevError !== "invoke_failed") {
        notifStore.push({ type: "error", title: "Claude usage unavailable", body: String(e) });
      }
    }
    return;
  }

  try {
    const j = await invoke<{ ok: boolean; usage?: PlanUsage; error?: string; message?: string }>("claude_plan_usage", args);
    if (j?.ok && j.usage) {
      planUsage.value = j.usage;
      localUsage.value = null;
      usageError.value = null;
      usageStale.value = false;
    } else {
      const err = j?.error || "unknown";
      if (LOCAL_FALLBACK_ERRORS.has(err)) {
        // Missing/expired credentials — read local transcripts instead. An expired
        // token (token_expired) also flags the profile as stale so the UI hints.
        planUsage.value = null;
        usageError.value = null;
        usageStale.value = err === "token_expired";
        const local = await invoke<{ outputTokens: number; turnCount: number }>(
          "claude_usage_5h",
          cd ? { configDir: cd } : {},
        );
        localUsage.value = local;
      } else {
        localUsage.value = null;
        usageStale.value = false;
        usageError.value = err;
        if (err !== prevError) {
          const pname = profile?.name ?? "Default";
          const body = j?.message ?? err;
          notifStore.push({ type: "error", title: "Claude usage unavailable", body: `${body} (${pname})` });
        }
      }
    }
  } catch (e) {
    usageError.value = "invoke_failed";
    usageStale.value = false;
    if (prevError !== "invoke_failed") {
      notifStore.push({ type: "error", title: "Claude usage unavailable", body: String(e) });
    }
  }
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
}

// One bar per limit window. Model-specific weekly bars only appear once used —
// they read 0% on plans that don't split per-model, so showing them is noise.
// For local fallback: single synthetic bar showing token count (no % available).
// Extra usage: pay-per-use credit meter shown when is_enabled=true.
const usageBars = computed<UsageBar[]>(() => {
  if (localUsage.value) {
    const { outputTokens, turnCount } = localUsage.value;
    if (outputTokens === 0 && turnCount === 0) return [];
    return [{ key: "local_5h", label: fmtTokens(outputTokens), pct: 0, local: true }];
  }
  const u = planUsage.value;
  if (!u) return [];
  const out: UsageBar[] = [];
  const add = (key: string, label: string, hideZero = false) => {
    const w = u[key] as UsageWindow | undefined;
    if (!w || w.utilization === null) return;
    const pct = Math.round(w.utilization || 0);
    if (hideZero && pct <= 0) return;
    out.push({ key, label, pct, resets: w.resets_at });
  };
  add("five_hour", "5h");
  add("seven_day", "wk");
  add("seven_day_sonnet", "son", true);
  add("seven_day_opus", "opus", true);
  add("seven_day_oauth_apps", "apps", true);
  // Pay-per-use credit meter
  const ex = u.extra_usage as ExtraUsage | undefined;
  if (ex?.is_enabled && ex.monthly_limit && ex.used_credits !== undefined) {
    const pct = Math.round(ex.utilization || 0);
    out.push({ key: "extra_usage", label: `$${ex.used_credits.toFixed(2)}`, pct, credit: true });
  }
  return out;
});

function usageSeverity(pct: number): string {
  if (pct >= 85) return "crit";
  if (pct >= 60) return "warn";
  return "";
}

function relTimeFuture(iso?: string): string {
  if (!iso) return "";
  let s = Math.round((new Date(iso).getTime() - Date.now()) / 1000);
  if (s <= 0) return "now";
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60), mm = m % 60;
  if (h < 24) return mm ? `${h}h ${mm}m` : `${h}h`;
  const d = Math.floor(h / 24), hh = h % 24;
  return hh ? `${d}d ${hh}h` : `${d}d`;
}

function usageBarTitle(b: UsageBar): string {
  if (b.local) {
    const lu = localUsage.value;
    return `5h output tokens (local): ${fmtTokens(lu?.outputTokens ?? 0)} across ${lu?.turnCount ?? 0} turns\nUsage API unavailable for this account — reading local transcripts`;
  }
  if (b.credit) {
    const ex = (planUsage.value as any)?.extra_usage as ExtraUsage | undefined;
    return `Pay-per-use: $${ex?.used_credits?.toFixed(2) ?? "?"} of $${ex?.monthly_limit?.toFixed(2) ?? "?"}/mo used (${b.pct}%)`;
  }
  const reset = b.resets ? ` · resets in ${relTimeFuture(b.resets)}` : "";
  return `${b.label}: ${b.pct}% used${reset}`;
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

async function openIn(target: OpenInTarget) {
  menuOpen.value = false;
  if (!props.folderPath) return;
  lastOpenIn.value = target;
  setConfig(LAST_OPEN_IN_CONFIG_KEY, target);
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
  branchPickerOpen.value = false;
  usageProfileMenuOpen.value = false;
  if (statsOpen.value) { statsOpen.value = false; clearInterval(statsTimer); }
}
onMounted(() => {
  window.addEventListener("click", onDocClick);
  refreshUsage();
  usageTimer = window.setInterval(refreshUsage, 60_000);
});
onBeforeUnmount(() => {
  window.removeEventListener("click", onDocClick);
  clearInterval(statsTimer);
  clearInterval(usageTimer);
});

const isDev = import.meta.env.DEV;
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

.usage-bar-warn { color: var(--yellow); }
.usage-bar-warn .ub-label { color: var(--yellow); }
.usage-bar-warn .ub-fill { background: var(--yellow); }

.usage-bar-crit { color: var(--red); animation: usage-pulse 1.6s ease-in-out infinite; }
.usage-bar-crit .ub-label { color: var(--red); }
.usage-bar-crit .ub-fill { background: var(--red); }
.usage-bar-crit .ub-track { border-color: var(--red); background: rgba(248,81,73,0.18); }
@keyframes usage-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.45; } }
@media (prefers-reduced-motion: reduce) { .usage-bar-crit { animation: none; } }
/* Credit bar: dollar label in amber, track uses amber fill */
.usage-bar-credit .ub-label { color: #f59e0b; font-style: normal; }
.usage-bar-credit .ub-fill { background: #f59e0b; }
</style>
