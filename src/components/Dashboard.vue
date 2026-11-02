<!--
  Dashboard.vue — Mission Control (route /dashboard).

  Every agent across every workspace — terminal agent tabs and top-level chats —
  sorted into three lanes: Needs you · Running · Ready for review. A PTY
  permission request is answerable right on its card (the same
  ptyPermissions store PtyPermissionBanner uses); every other card opens its
  tab or chat.

  Sources, all existing: terminalTabs (each opened workspace's tab mirror, whose
  `status` is already the client's displayStatus), chatAttention (chat status +
  phase), `phase-pty:<id>` for a terminal agent's detail/timestamps, and
  `run_git` for a review card's diffstat.
-->
<template>
  <div class="surface-glow flex h-full flex-col overflow-hidden bg-base text-foreground">
    <header class="flex shrink-0 flex-wrap items-end gap-x-6 gap-y-3 px-6 pb-4 pt-5">
      <div class="min-w-0">
        <h1 class="m-0 text-[19px] font-semibold tracking-[-0.01em]">Mission Control</h1>
        <p class="m-0 mt-1 text-[12px] text-muted-foreground">
          {{ cards.length }} {{ cards.length === 1 ? "agent" : "agents" }} · {{ projectCount }} {{ projectCount === 1 ? "project" : "projects" }}
        </p>
      </div>
      <span class="flex-1" />
      <div class="flex items-center gap-3">
      <nav class="flex items-center gap-0.5" aria-label="Filter agents">
        <button
          v-for="f in FILTERS"
          :key="f.id"
          class="flex h-7 items-center gap-1.5 rounded-lg px-2.5 text-[12px] transition-colors"
          :class="filter === f.id ? 'bg-selected text-foreground' : 'text-muted-foreground hover:bg-hover hover:text-secondary-foreground'"
          @click="filter = f.id"
        >
          {{ f.label }}
          <span v-if="f.id !== 'all' && lanes[f.id].length" class="tabular-nums text-muted-foreground">{{ lanes[f.id].length }}</span>
        </button>
      </nav>
      <button
        class="flex h-7 items-center gap-1.5 rounded-lg border border-border px-2.5 text-[12px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
        @click="$emit('new-workspace')"
      >
        <PhPlus :size="12" /> New project
      </button>
      </div>
    </header>

    <div class="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-6 pb-6">
      <!-- ── Stat tiles ─────────────────────────────────────────────────── -->
      <div class="grid shrink-0 grid-cols-3 gap-3">
        <button
          v-for="l in LANES"
          :key="l.id"
          class="flex flex-col gap-1 rounded-xl border border-border/70 px-4 py-3 text-left transition-colors hover:bg-hover"
          :class="filter === l.id && 'border-border bg-hover'"
          @click="filter = filter === l.id ? 'all' : l.id"
        >
          <span class="flex items-center gap-1.5 text-[11.5px] text-muted-foreground"><em class="dot" :class="l.dot" />{{ l.label }}</span>
          <span class="text-[24px] font-semibold tabular-nums leading-tight" :class="!lanes[l.id].length && 'text-muted-foreground'">{{ lanes[l.id].length }}</span>
        </button>
      </div>

      <!-- ── Lanes ──────────────────────────────────────────────────────── -->
      <div class="grid min-h-0 gap-4" :class="visibleLanes.length === 1 ? 'grid-cols-1' : 'grid-cols-[repeat(auto-fit,minmax(200px,1fr))]'">
        <section v-for="l in visibleLanes" :key="l.id" class="flex min-w-0 flex-col gap-2">
          <h2 class="m-0 flex items-center gap-2 px-0.5 text-[12px] font-medium text-secondary-foreground">
            {{ l.label }} <span class="tabular-nums text-muted-foreground">{{ lanes[l.id].length }}</span>
          </h2>
          <p v-if="!lanes[l.id].length" class="m-0 rounded-xl border border-dashed border-border/60 px-4 py-5 text-center text-[12px] text-muted-foreground">{{ l.empty }}</p>

          <div
            v-for="c in lanes[l.id]"
            :key="c.key"
            role="button"
            tabindex="0"
            class="group flex cursor-pointer flex-col gap-2 rounded-xl border border-border/70 bg-[color-mix(in_srgb,var(--text-primary)_2%,transparent)] px-3.5 py-3 text-left transition-colors hover:border-border hover:bg-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
            @click="openCard(c)"
            @keydown.enter="openCard(c)"
          >
            <div class="flex items-center gap-2">
              <component :is="agentIconComp(c.icon)" :size="14" class="shrink-0 text-secondary-foreground" />
              <span class="min-w-0 flex-1 truncate text-[13px] font-medium">{{ c.title }}</span>
              <em class="dot" :class="c.status" />
            </div>
            <div class="flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <span class="max-w-[60%] shrink-0 truncate">{{ c.wsName }}</span>
              <template v-if="c.branch"><span>·</span><PhGitBranch :size="11" class="shrink-0" /><span class="truncate">{{ c.branch }}</span></template>
            </div>

            <!-- Needs you: the request itself -->
            <template v-if="l.id === 'needs'">
              <div v-if="c.perm" class="flex flex-col gap-2">
                <div class="rounded-lg border border-border/70 bg-base/60 px-2.5 py-2">
                  <div class="text-[11px] font-medium text-secondary-foreground">{{ c.perm.head.tool_name }}<span v-if="c.perm.more" class="text-muted-foreground"> · +{{ c.perm.more }} more</span></div>
                  <div class="mt-0.5 line-clamp-2 break-all font-mono text-[11px] text-muted-foreground">{{ describePermission(c.perm.head) }}</div>
                </div>
                <PtyPermissionActions :request="c.perm.head" @click.stop />
              </div>
              <div v-else class="line-clamp-2 text-[12px] text-secondary-foreground">
                {{ c.detail || (c.status === "error" ? "The last turn failed." : c.status === "permission" ? "Waiting for permission." : "Waiting for your answer.") }}
              </div>
            </template>

            <!-- Running: elapsed + activity + progress line -->
            <template v-else-if="l.id === 'running'">
              <div class="flex items-center gap-2 text-[12px]">
                <span class="min-w-0 flex-1 truncate text-secondary-foreground">{{ c.detail || "Working…" }}</span>
                <span class="shrink-0 tabular-nums text-muted-foreground">{{ elapsed(c.since) }}</span>
              </div>
              <div class="relative h-0.5 overflow-hidden rounded-full bg-border/60"><div class="mc-progress" /></div>
            </template>

            <!-- Review: diffstat or the last line -->
            <template v-else>
              <div class="flex items-center gap-2 text-[12px] text-muted-foreground">
                <span v-if="gitByWs[c.wsId]?.dirty" class="shrink-0 text-secondary-foreground">{{ gitByWs[c.wsId].dirty }} {{ gitByWs[c.wsId].dirty === 1 ? "file" : "files" }} changed</span>
                <span v-else class="min-w-0 truncate">{{ c.detail || "Turn finished" }}</span>
                <span class="flex-1" />
                <span v-if="c.since" class="shrink-0 tabular-nums">{{ ago(c.since) }}</span>
              </div>
            </template>
          </div>
        </section>
      </div>

      <p v-if="!cards.length" class="m-0 text-center text-[12px] text-muted-foreground">
        No agents are working right now. Start one from a project's thread or terminal.
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted, onBeforeUnmount, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { PhGitBranch, PhPlus } from "@phosphor-icons/vue";
import PtyPermissionActions from "@/components/PtyPermissionActions.vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useChatAttentionStore } from "@/stores/chatAttention";
import { useProvidersStore } from "@/stores/providers";
import { usePtyPermissionsStore, describePermission, headOfQueue, type PtyPermission } from "@/stores/ptyPermissions";
import { agentIconComp } from "@/lib/agentIcons";
import { router } from "@/router";
import type { TermStatus } from "@/lib/terminalStatus";
import type { Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";

defineEmits<{ (e: "new-workspace"): void }>();

const ws = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const chats = useClaudeChatsStore();
const chatAttention = useChatAttentionStore();
const providers = useProvidersStore();
const perms = usePtyPermissionsStore();
perms.start();

type LaneId = "needs" | "running" | "review";
const LANES: { id: LaneId; label: string; dot: TermStatus; empty: string }[] = [
  { id: "needs", label: "Needs you", dot: "permission", empty: "Nothing is waiting on you." },
  { id: "running", label: "Running", dot: "running", empty: "No agent is working." },
  { id: "review", label: "Ready for review", dot: "review", empty: "Nothing to review." },
];
const FILTERS: { id: LaneId | "all"; label: string }[] = [
  { id: "all", label: "All" },
  { id: "needs", label: "Needs you" },
  { id: "running", label: "Running" },
  { id: "review", label: "Review" },
];
const filter = ref<LaneId | "all">("all");
const visibleLanes = computed(() => (filter.value === "all" ? LANES : LANES.filter((l) => l.id === filter.value)));

function laneOf(status: TermStatus): LaneId | null {
  if (status === "permission" || status === "waiting" || status === "error") return "needs";
  if (status === "running") return "running";
  if (status === "review" || status === "done") return "review";
  return null;
}

interface Card {
  key: string;
  wsId: number;
  wsName: string;
  title: string;
  icon?: string;
  branch?: string;
  status: TermStatus;
  detail: string;
  /** When the card entered its current state (ms), for elapsed / "ago". */
  since: number;
  tabId?: number;
  chatId?: number;
  perm?: { head: PtyPermission; more: number };
}

// ── Terminal agent phases: one snapshot for the start, then live events ─────
const ptyPhases = reactive(new Map<number, Phase>());
const unlisteners = new Map<number, UnlistenFn>();
const ptyIds = computed(() =>
  Object.values(termTabs.tabsByWs).flat().filter((t) => !t.isChat).map((t) => t.id),
);
watch(ptyIds, (ids) => {
  const live = new Set(ids);
  for (const [id, off] of unlisteners) if (!live.has(id)) { off(); unlisteners.delete(id); ptyPhases.delete(id); }
  for (const id of ids) {
    if (unlisteners.has(id)) continue;
    unlisteners.set(id, () => {}); // reserve the slot while listen() resolves
    void listen<Phase>(`phase-pty:${id}`, (e) => ptyPhases.set(id, e.payload)).then((off) => {
      if (unlisteners.has(id)) unlisteners.set(id, off);
      else off();
    });
  }
}, { immediate: true });
onMounted(() => {
  void invoke<ShellSnapshotData>("shell_snapshot").then((snap) => {
    for (const [key, phase] of Object.entries(snap.phases ?? {})) {
      const id = Number(key.startsWith("pty:") ? key.slice(4) : NaN);
      if (Number.isFinite(id) && !ptyPhases.has(id)) ptyPhases.set(id, phase);
    }
  }).catch(() => {});
});

// ── Cards ────────────────────────────────────────────────────────────────────
const wsName = (id: number) => ws.workspaces.find((w) => w.id === id)?.name ?? "?";

const cards = computed<Card[]>(() => {
  const out: Card[] = [];
  for (const [wsKey, tabs] of Object.entries(termTabs.tabsByWs)) {
    const wsId = Number(wsKey);
    for (const t of tabs ?? []) {
      if (t.isChat) continue;
      // A parked permission request IS "needs you": Go closes it the moment the
      // phase leaves waiting_approval, so it never outlives the prompt.
      const perm = headOfQueue(perms.byPty[String(t.id)] ?? []) ?? undefined;
      const status: TermStatus = perm ? "permission" : t.status;
      if (!laneOf(status)) continue;
      const phase = ptyPhases.get(t.id);
      out.push({
        key: `pty:${t.id}`, wsId, wsName: wsName(wsId), title: t.title, icon: t.agentIcon, branch: t.branch,
        status, detail: phase?.detail ?? "", since: perm?.head.at ?? phase?.updated_at ?? 0, tabId: t.id, perm,
      });
    }
  }
  // Top-level chats only: a sub-agent belongs to its thread's Right Panel.
  for (const s of chats.sessions) {
    if (s.archivedAt || s.parentChatId) continue;
    const status = chatAttention.statusFor(s.id);
    if (!laneOf(status)) continue;
    const phase = chatAttention.phaseFor(s.id);
    out.push({
      key: `chat:${s.id}`, wsId: s.workspaceId, wsName: wsName(s.workspaceId), title: s.title,
      icon: providers.resolve(s.agentKind)?.icon, branch: s.branch, status,
      detail: phase?.detail ?? "", since: phase?.updated_at ?? s.lastActivityAt ?? 0, chatId: s.id,
    });
  }
  return out;
});

const URGENCY: TermStatus[] = ["error", "permission", "waiting", "running", "review", "done"];
const lanes = computed(() => {
  const by: Record<LaneId, Card[]> = { needs: [], running: [], review: [] };
  for (const c of cards.value) by[laneOf(c.status)!].push(c);
  // Most urgent first, then the oldest wait / the newest finish.
  by.needs.sort((a, b) => URGENCY.indexOf(a.status) - URGENCY.indexOf(b.status) || a.since - b.since);
  by.running.sort((a, b) => a.since - b.since);
  by.review.sort((a, b) => b.since - a.since);
  return by;
});
const projectCount = computed(() => new Set(cards.value.map((c) => c.wsId)).size);

function openCard(c: Card) {
  const target = ws.workspaces.find((w) => w.id === c.wsId);
  if (target) ws.open(target);
  const tabId = c.tabId ?? termTabs.tabsByWs[c.wsId]?.find((t) => t.chatId === c.chatId)?.id;
  if (tabId != null) {
    void router.push(`/ws/${c.wsId}/tab/${tabId}`);
    return;
  }
  // A chat with no open tab: the workspace's Terminal opens it and focuses it.
  if (c.chatId != null) termTabs.openChat(c.wsId, c.chatId);
  void router.push(`/ws/${c.wsId}`);
}

// ── Time ─────────────────────────────────────────────────────────────────────
// ponytail: `since` is the phase's updated_at — a mid-turn detail change resets
// it, so "elapsed" is time in the current activity, not the whole turn.
const now = ref(Date.now());
function elapsed(since: number): string {
  if (!since) return "";
  const s = Math.max(0, Math.floor((now.value - since) / 1000));
  return s < 60 ? `${s}s` : s < 3600 ? `${Math.floor(s / 60)}m ${s % 60}s` : `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}
function ago(since: number): string {
  const m = Math.floor((now.value - since) / 60_000);
  return m < 1 ? "just now" : m < 60 ? `${m}m ago` : m < 1440 ? `${Math.floor(m / 60)}h ago` : `${Math.floor(m / 1440)}d ago`;
}

// ── Diffstat for review cards ────────────────────────────────────────────────
interface GitOutput { stdout: string; stderr: string; code: number; }
const gitByWs = reactive<Record<number, { dirty: number }>>({});
async function refreshGit() {
  const ids = [...new Set(lanes.value.review.map((c) => c.wsId))];
  await Promise.all(ids.map(async (id) => {
    const path = ws.workspaces.find((w) => w.id === id)?.path;
    if (!path) return;
    const out = await invoke<GitOutput>("run_git", { cwd: path, args: ["status", "--porcelain"] }).catch(() => null);
    if (out?.code === 0) gitByWs[id] = { dirty: out.stdout.split("\n").filter((l) => l.trim()).length };
  }));
}
watch(() => lanes.value.review.map((c) => c.wsId).join(","), () => void refreshGit(), { immediate: true });

let tick: number | undefined;
let gitTimer: number | undefined;
onMounted(() => {
  tick = window.setInterval(() => (now.value = Date.now()), 1000);
  gitTimer = window.setInterval(() => void refreshGit(), 10_000);
});
onBeforeUnmount(() => {
  clearInterval(tick);
  clearInterval(gitTimer);
  for (const off of unlisteners.values()) off();
  unlisteners.clear();
});
</script>

<style scoped>
/* Status dots: dynamic color + glow + pulse per status. */
.dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; background: var(--text-muted); flex-shrink: 0; }
.dot.running { background: var(--yellow); box-shadow: 0 0 6px color-mix(in srgb, var(--yellow) 50%, transparent); animation: pulse 1.4s infinite; }
.dot.waiting { background: var(--accent); }
.dot.permission { background: var(--accent); box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 50%, transparent); animation: pulse 1.4s infinite; }
.dot.review { background: var(--green); }
.dot.done { background: var(--green); box-shadow: 0 0 6px color-mix(in srgb, var(--green) 50%, transparent); }
.dot.error { background: var(--red); }

/* Indeterminate: no agent reports real progress, so this only says "alive". */
.mc-progress {
  position: absolute; inset: 0 auto 0 0; width: 35%;
  border-radius: 9999px; background: var(--text-secondary); opacity: .55;
  animation: mc-slide 1.8s ease-in-out infinite;
}
@keyframes mc-slide { from { transform: translateX(-100%); } to { transform: translateX(290%); } }
@keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .4; } }
</style>
