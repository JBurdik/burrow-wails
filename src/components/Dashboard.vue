<!--
  Dashboard.vue — the "home" landing view (first activity-bar item).

  A full main-pane mode (ui.mode === 'dashboard'), same slot as GitPanel /
  the main pane. Four panels:
    1. Agent activity   — cross-workspace roll-up of terminal-tab statuses
                          (running/waiting/review/done) + an "attention" list.
    2. Quick actions    — launcher buttons (new terminal/chat/workspace,
                          settings).
    3. Workspaces       — card grid of every top-level repo + its worktrees, with
                          live git branch / dirty count, click to open.
  Reads the workspace + terminalTabs stores; pulls per-workspace git state via the
  `run_git` Tauri command directly (the git store only tracks the active cwd).
-->
<template>
  <div class="flex h-full flex-col overflow-hidden bg-base text-foreground">
    <header class="flex shrink-0 items-center gap-2.5 border-b border-border px-5 py-3.5">
      <PhSquaresFour :size="20" weight="bold" class="text-accent" />
      <h1 class="m-0 text-base font-[650]">Dashboard</h1>
      <span class="flex-1" />
      <button
        class="inline-flex items-center gap-1.5 rounded-md border border-border bg-transparent px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground"
        @click="refreshGit"
        :disabled="gitBusy"
      >
        <PhArrowsClockwise :size="13" :class="{ 'animate-spin': gitBusy }" /> refresh
      </button>
    </header>

    <div class="grid flex-1 auto-rows-min grid-cols-2 gap-4 overflow-y-auto p-5">
      <!-- ── Agent activity ──────────────────────────────────────────── -->
      <section class="rounded-xl border border-border bg-panel p-4 [backdrop-filter:var(--blur-content,none)]">
        <h2 class="m-0 mb-3 text-xs font-semibold uppercase tracking-[0.04em] text-muted-foreground">Agent activity</h2>
        <div class="mb-3 flex flex-wrap gap-2">
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.running }"><em class="dot running" />{{ counts.running }}<small class="font-medium text-[11px] text-muted-foreground">running</small></span>
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.permission }"><em class="dot permission" />{{ counts.permission }}<small class="font-medium text-[11px] text-muted-foreground">permission</small></span>
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.waiting }"><em class="dot waiting" />{{ counts.waiting }}<small class="font-medium text-[11px] text-muted-foreground">waiting</small></span>
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.error }"><em class="dot error" />{{ counts.error }}<small class="font-medium text-[11px] text-muted-foreground">error</small></span>
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.review }"><em class="dot review" />{{ counts.review }}<small class="font-medium text-[11px] text-muted-foreground">review</small></span>
          <span class="dot-chip inline-flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[13px] font-semibold opacity-55" :class="{ 'opacity-100': counts.done }"><em class="dot done" />{{ counts.done }}<small class="font-medium text-[11px] text-muted-foreground">done</small></span>
        </div>

        <div v-if="attention.length" class="flex flex-col gap-1">
          <button
            v-for="a in attention"
            :key="a.wsId + ':' + a.tabId"
            class="flex w-full items-center gap-2 rounded-[7px] border-0 bg-transparent px-2 py-1.5 text-left text-foreground hover:bg-hover"
            @click="goToTab(a)"
          >
            <em class="dot" :class="a.status" />
            <span class="flex-1 truncate text-[13px]">{{ a.title }}</span>
            <span class="text-[11px] text-muted-foreground">{{ a.wsName }}</span>
          </button>
        </div>
        <p v-else class="m-0 mt-1 text-[13px] text-muted-foreground">No agents need attention.</p>
      </section>

      <!-- ── Quick actions ───────────────────────────────────────────── -->
      <section class="rounded-xl border border-border bg-panel p-4 [backdrop-filter:var(--blur-content,none)]">
        <h2 class="m-0 mb-3 text-xs font-semibold uppercase tracking-[0.04em] text-muted-foreground">Quick actions</h2>
        <div class="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-2">
          <button class="flex flex-col items-center gap-1.5 rounded-[10px] border border-transparent bg-hover px-2 py-3.5 text-xs font-[550] text-foreground transition-colors hover:border-accent hover:text-accent disabled:cursor-default disabled:opacity-40" :disabled="!ws.active" @click="newTerminal">
            <PhTerminal :size="18" /><span>New terminal</span>
          </button>
          <button class="flex flex-col items-center gap-1.5 rounded-[10px] border border-transparent bg-hover px-2 py-3.5 text-xs font-[550] text-foreground transition-colors hover:border-accent hover:text-accent disabled:cursor-default disabled:opacity-40" :disabled="!ws.active" @click="newChat">
            <ClaudeIcon :size="18" /><span>New chat</span>
          </button>
          <button class="flex flex-col items-center gap-1.5 rounded-[10px] border border-transparent bg-hover px-2 py-3.5 text-xs font-[550] text-foreground transition-colors hover:border-accent hover:text-accent" @click="$emit('new-workspace')">
            <PhFolderPlus :size="18" /><span>New workspace</span>
          </button>
          <button class="flex flex-col items-center gap-1.5 rounded-[10px] border border-transparent bg-hover px-2 py-3.5 text-xs font-[550] text-foreground transition-colors hover:border-accent hover:text-accent" @click="ui.openSettings()">
            <PhGear :size="18" /><span>Settings</span>
          </button>
        </div>
      </section>

      <!-- ── Workspaces ──────────────────────────────────────────────── -->
      <section class="col-span-full rounded-xl border border-border bg-panel p-4 [backdrop-filter:var(--blur-content,none)]">
        <h2 class="m-0 mb-3 text-xs font-semibold uppercase tracking-[0.04em] text-muted-foreground">Workspaces <small class="font-medium text-muted-foreground opacity-60">{{ ws.topLevel.length }}</small></h2>
        <div v-if="!ws.topLevel.length" class="mt-1 text-[13px] text-muted-foreground">No workspaces yet.</div>
        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-2.5">
          <button
            v-for="w in ws.topLevel"
            :key="w.id"
            class="flex flex-col gap-1.5 rounded-[10px] border border-border bg-hover px-3.5 py-3 text-left text-foreground transition-colors hover:border-accent"
            :class="{ 'border-accent shadow-[0_0_0_1px_var(--accent)_inset]': ws.active?.id === w.id }"
            @click="openWs(w)"
          >
            <div class="flex items-center gap-2">
              <img v-if="ws.icons[w.id]" class="h-4 w-4 shrink-0 rounded object-cover" :src="ws.icons[w.id]" alt="" />
              <PhFolder v-else :size="16" weight="fill" class="shrink-0 text-muted-foreground" />
              <span class="flex-1 truncate text-[13px] font-semibold">{{ w.name }}</span>
              <em v-if="wsAgg(w.id) !== 'idle'" class="dot" :class="wsAgg(w.id)" :title="wsAgg(w.id)" />
            </div>
            <div class="flex items-center gap-2 text-[11px] text-muted-foreground">
              <span class="inline-flex items-center gap-0.5" v-if="gitByWs[w.id]?.branch">
                <PhGitBranch :size="12" />{{ gitByWs[w.id].branch }}
              </span>
              <span class="font-semibold text-warning" v-if="gitByWs[w.id]?.dirty" :title="gitByWs[w.id].dirty + ' changed files'">
                ●{{ gitByWs[w.id].dirty }}
              </span>
              <span class="text-accent" v-if="gitByWs[w.id]?.ahead">↑{{ gitByWs[w.id].ahead }}</span>
              <span class="text-accent" v-if="gitByWs[w.id]?.behind">↓{{ gitByWs[w.id].behind }}</span>
            </div>
            <div class="truncate text-[11px] text-muted-foreground opacity-70">{{ shortCwd(w.path) }}</div>
            <!-- worktrees nested under the repo -->
            <ul v-if="(ws.worktreesByParent[w.id] || []).length" class="m-0 flex list-none flex-col gap-0.5 border-t border-border pt-1.5" @click.stop>
              <li
                v-for="wt in ws.worktreesByParent[w.id]"
                :key="wt.id"
                class="flex items-center gap-1.5 rounded-[5px] px-1 py-0.5 text-[11px] text-muted-foreground hover:bg-panel hover:text-foreground"
                @click="openWs(wt)"
              >
                <PhGitBranch :size="11" />
                <span class="flex-1 truncate">{{ wt.worktree_branch || wt.name }}</span>
                <em v-if="wsAgg(wt.id) !== 'idle'" class="dot" :class="wsAgg(wt.id)" />
              </li>
            </ul>
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted, onBeforeUnmount, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhSquaresFour, PhTerminal, PhFolder, PhFolderPlus, PhGitBranch,
  PhGear, PhArrowsClockwise,
} from "@phosphor-icons/vue";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { router } from "@/router";
import { aggregateStatus, type TermStatus } from "@/lib/terminalStatus";

defineEmits<{ (e: "new-workspace"): void }>();

const ws = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();

// ── Agent activity ──────────────────────────────────────────────────────────
// Flatten every workspace's tab summaries into one list (tagged with workspace).
const allTabs = computed(() =>
  Object.entries(termTabs.tabsByWs).flatMap(([wsId, tabs]) =>
    (tabs || []).map((t) => ({ ...t, wsId: Number(wsId) })),
  ),
);

const counts = computed(() => {
  const c = { running: 0, permission: 0, waiting: 0, error: 0, review: 0, done: 0 };
  for (const t of allTabs.value) {
    if (t.status in c) (c as Record<string, number>)[t.status]++;
  }
  return c;
});

// Tabs that want the user's eyes, highest-priority first.
const ATTN_ORDER: TermStatus[] = ["error", "permission", "waiting", "review", "done"];
const attention = computed(() => {
  const wsName = (id: number) => ws.workspaces.find((w) => w.id === id)?.name ?? "?";
  return allTabs.value
    .filter((t) => ATTN_ORDER.includes(t.status))
    .map((t) => ({ wsId: t.wsId, tabId: t.id, title: t.title, status: t.status, wsName: wsName(t.wsId) }))
    .sort((a, b) => ATTN_ORDER.indexOf(a.status) - ATTN_ORDER.indexOf(b.status));
});

// Per-workspace aggregate status for the card dot.
function wsAgg(wsId: number): TermStatus {
  const tabs = termTabs.tabsByWs[wsId] || [];
  return aggregateStatus(tabs, (t) => t.status);
}

function goToTab(a: { wsId: number; tabId: number }) {
  const target = ws.workspaces.find((w) => w.id === a.wsId);
  if (target) ws.open(target);
  // One navigation instead of "switch mode, then remember to activate the tab
  // 60 ms later": the route names both, and App.vue's route watcher activates it.
  void router.push(`/ws/${a.wsId}/tab/${a.tabId}`);
}

// ── Quick actions ─────────────────────────────────────────────────────────────
function newTerminal() {
  if (!ws.active) return;
  termTabs.add(ws.active.id);
  ui.closeWelcome(); // leave the dashboard for the workspace that just got the tab
}
function newChat() {
  if (!ws.active) return;
  termTabs.openChat(ws.active.id);
  ui.closeWelcome(); // leave the dashboard for the workspace that just got the tab
}
function openWs(w: Workspace) {
  ws.open(w);
  ui.setMode("terminal");
}

// ── Per-workspace git state ─────────────────────────────────────────────────
interface GitInfo { branch: string; dirty: number; ahead: number; behind: number; }
const gitByWs = reactive<Record<number, GitInfo>>({});
const gitBusy = ref(false);

interface GitOutput { stdout: string; stderr: string; code: number; }

async function gitFor(path: string): Promise<GitInfo | null> {
  try {
    const [status, branch, upstream] = await Promise.all([
      invoke<GitOutput>("run_git", { cwd: path, args: ["status", "--porcelain"] }),
      invoke<GitOutput>("run_git", { cwd: path, args: ["branch", "--show-current"] }),
      invoke<GitOutput>("run_git", { cwd: path, args: ["rev-list", "--left-right", "--count", "@{upstream}...HEAD"] }),
    ]);
    if (branch.code !== 0) return null; // not a git repo
    const dirty = status.stdout.split("\n").filter((l) => l.trim().length > 0).length;
    let ahead = 0, behind = 0;
    if (upstream.code === 0) {
      const [b, a] = upstream.stdout.trim().split(/\s+/);
      behind = parseInt(b, 10) || 0;
      ahead = parseInt(a, 10) || 0;
    }
    return { branch: branch.stdout.trim(), dirty, ahead, behind };
  } catch {
    return null; // browser-only dev (no Tauri) or non-repo
  }
}

async function refreshGit() {
  if (gitBusy.value) return;
  gitBusy.value = true;
  try {
    const targets = [
      ...ws.topLevel,
      ...Object.values(ws.worktreesByParent).flat(),
    ];
    await Promise.all(
      targets.map(async (w) => {
        const info = await gitFor(w.path);
        if (info) gitByWs[w.id] = info;
        else delete gitByWs[w.id];
      }),
    );
  } finally {
    gitBusy.value = false;
  }
}

// ── Misc ────────────────────────────────────────────────────────────────────
function shortCwd(p: string): string {
  const home = "/Users/";
  let s = p;
  if (s.startsWith(home)) {
    const rest = s.slice(home.length).split("/").slice(1).join("/");
    s = "~/" + rest;
  }
  return s;
}

// Refresh git on mount and on a slow poll while the dashboard is visible.
let timer: number | undefined;
onMounted(() => {
  refreshGit();
  timer = window.setInterval(() => {
    if (ui.mode === "dashboard") refreshGit();
  }, 8000);
});
onBeforeUnmount(() => { if (timer) clearInterval(timer); });

// Re-scan when workspaces change (created/removed) while open.
watch(() => ws.workspaces.length, () => refreshGit());
</script>

<style scoped>
/* Status dots: dynamic color + glow (box-shadow color-mix) + pulse animation
   per status — kept as CSS classes rather than force-fit into Tailwind. */
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; background: var(--text-muted); flex-shrink: 0; }
.dot.running { background: var(--yellow); box-shadow: 0 0 8px color-mix(in srgb, var(--yellow) 53%, transparent); animation: pulse 1.4s infinite; }
.dot.waiting { background: var(--accent); box-shadow: 0 0 8px color-mix(in srgb, var(--accent) 53%, transparent); }
.dot.permission { background: var(--accent); box-shadow: 0 0 8px color-mix(in srgb, var(--accent) 53%, transparent); animation: pulse 1.4s infinite; }
.dot.review { background: var(--green); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 53%, transparent); animation: pulse 1.8s infinite; }
.dot.done { background: var(--green); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 53%, transparent); }
.dot.error { background: var(--red); box-shadow: 0 0 8px color-mix(in srgb, var(--red) 60%, transparent); animation: pulse 1.4s infinite; }

@keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .4; } }
</style>
