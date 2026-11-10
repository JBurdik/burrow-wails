<template>
  <!-- Thread details (after t3code's ThreadDetailsCard): where this thread runs,
       then its git state. Rows come from visibleRows() in lib/threadDetails. -->
  <div ref="rootEl" class="details-card absolute right-2 top-2 z-[60] flex w-[288px] flex-col rounded-lg border border-border py-1 text-[12px]">
    <template v-for="row in rows" :key="row">
      <div v-if="row === firstGitRow" class="my-1 h-px bg-border" />

      <!-- Workspace / worktree: open in the last app; ▾ = other apps, path, worktree -->
      <div v-if="row === 'workspace'" class="dc-row">
        <component :is="workspace.parent_id ? PhGitFork : PhFolder" :size="14" class="dc-icon" />
        <span class="min-w-0 flex-1 truncate text-foreground" :title="workspace.path">{{ workspace.name }}</span>
        <button v-if="!isWeb" class="dc-btn" :title="lastOpenTarget.id === 'finder' ? 'Reveal in Finder' : `Open in ${lastOpenTarget.name}`" @click="openIn(lastOpenTarget.id)">
          <img v-if="lastOpenTarget.icon" :src="lastOpenTarget.icon" class="h-3.5 w-3.5 shrink-0" />
          <PhFolderOpen v-else :size="13" />
        </button>
        <div class="relative">
          <button class="dc-btn" title="Open in…" @click.stop="toggle('ws')"><PhCaretDown :size="10" /></button>
          <div v-if="menu === 'ws'" class="dc-menu" @click.stop>
            <template v-if="!isWeb">
              <button v-for="t in openTargets" :key="t.id" class="dc-menu-item" :class="lastOpenIn === t.id && '!text-accent'" @click="openIn(t.id)">
                <img v-if="t.icon" :src="t.icon" class="h-3.5 w-3.5 shrink-0" />
                <PhFolderNotchOpen v-else :size="13" />
                {{ t.id === "finder" ? "Reveal in Finder" : `Open in ${t.name}` }}
              </button>
            </template>
            <button class="dc-menu-item" @click="copyPath"><PhCopy :size="13" />Copy Path</button>
            <template v-if="chat">
              <div class="my-1 h-px bg-border" />
              <form v-if="!workspace.parent_id" class="flex flex-col gap-1 p-1" @submit.prevent="moveToWorktree">
                <span class="px-1 text-[11px] text-muted-foreground">Move to worktree…</span>
                <input v-model="moveBranch" class="dc-input" placeholder="Branch name" />
              </form>
              <button v-else class="dc-menu-item" :disabled="busy" @click="leaveWorktree"><PhArrowUUpLeft :size="13" />Leave worktree</button>
              <div v-if="moveError" class="px-2 pb-1 text-[11px] text-destructive">{{ moveError }}</div>
            </template>
          </div>
        </div>
      </div>

      <!-- Scripts -->
      <div v-else-if="row === 'scripts'" class="dc-row flex-wrap">
        <PhPlay :size="14" class="dc-icon" />
        <button v-for="sc in scripts" :key="sc.id" class="dc-btn" :title="scriptsStore.commandLine(sc)" @click="runScript(sc)">{{ sc.name }}</button>
        <button class="dc-btn" @click="$emit('open-config')"><PhPlus :size="11" />Add script</button>
      </div>

      <!-- Branch ▾ -->
      <div v-else-if="row === 'branch'" class="dc-row">
        <PhGitBranch :size="14" class="dc-icon" />
        <span class="min-w-0 flex-1 truncate text-foreground" :title="branch">{{ branch }}</span>
        <span v-if="gi.upstream && (gi.ahead || gi.behind)" class="shrink-0 text-muted-foreground" title="Ahead / behind upstream">↑{{ gi.ahead }} ↓{{ gi.behind }}</span>
        <span v-else-if="!gi.upstream" class="shrink-0 text-muted-foreground">not pushed</span>
        <div v-if="storeHere" class="relative">
          <button class="dc-btn" title="Switch branch" @click.stop="toggle('branch')"><PhCaretDown :size="10" /></button>
          <div v-if="menu === 'branch'" class="dc-menu max-h-[260px] overflow-y-auto" @click.stop>
            <button v-for="b in git.branches" :key="b" class="dc-menu-item" :class="b === branch && '!text-accent'" @click="switchBranch(b)">{{ b }}</button>
            <div v-if="branchError" class="px-2 pb-1 text-[11px] text-destructive">{{ branchError }}</div>
          </div>
        </div>
      </div>

      <!-- Pull request: the chat's linked one (chatpr.go) wins over the branch's -->
      <div v-else-if="row === 'pr'" class="dc-row" :class="pr && 'dc-click'">
        <button v-if="pr" class="flex min-w-0 flex-1 items-center gap-1.5" :title="pr.url" @click="$emit('open-pr', pr.number, pr.url)">
          <PhGitPullRequest :size="14" class="dc-icon" :class="pr.state.toUpperCase() === 'MERGED' ? 'text-accent' : pr.state.toUpperCase() === 'OPEN' ? 'text-success' : ''" />
          <span class="flex-1 truncate text-left text-foreground">#{{ pr.number }} <span class="text-muted-foreground">{{ pr.isDraft ? "draft" : pr.state.toLowerCase() }}</span></span>
          <span v-if="pr.checks !== 'none'" :class="CHECK_CLASS[pr.checks]">{{ CHECK_LABEL[pr.checks] }}</span>
        </button>
        <template v-else>
          <PhGitPullRequest :size="14" class="dc-icon" />
          <span class="flex-1 text-muted-foreground">No pull request</span>
        </template>
        <div v-if="chat" class="relative">
          <button class="dc-btn" title="Link pull request" @click.stop="toggle('pr')"><PhCaretDown :size="10" /></button>
          <div v-if="menu === 'pr'" class="dc-menu" @click.stop>
            <form class="flex flex-col gap-1 p-1" @submit.prevent="linkPr">
              <span class="px-1 text-[11px] text-muted-foreground">Link PR…</span>
              <input v-model="prInput" class="dc-input" placeholder="#123 or URL" autofocus />
            </form>
            <button v-if="chat.prNumber" class="dc-menu-item" :disabled="busy" @click="unlinkPr"><PhLinkBreak :size="13" />Unlink #{{ chat.prNumber }}</button>
            <div v-if="prError" class="px-2 pb-1 text-[11px] text-destructive">{{ prError }}</div>
          </div>
        </div>
      </div>

      <!-- Commit / push ▾ (was in the TitleBar) -->
      <div v-else-if="row === 'commit'" class="dc-row">
        <PhGitCommit :size="14" class="dc-icon" />
        <span v-if="gi.last" class="min-w-0 flex-1 truncate text-muted-foreground" :title="`${gi.last.shortHash} ${gi.last.subject} — ${gi.last.author}`">
          <span class="text-foreground">{{ gi.last.subject }}</span> · {{ gi.last.relTime }}
        </span>
        <span v-else class="flex-1 text-muted-foreground">Git</span>
        <CommitPushMenu v-if="storeHere" />
      </div>

      <!-- Changes (collapsible) -->
      <template v-else-if="row === 'changes'">
        <button class="dc-row dc-click" @click="toggleOpen('changes')">
          <component :is="open.changes ? PhCaretDown : PhCaretRight" :size="10" class="dc-caret" />
          <PhFileCode :size="14" class="dc-icon" />
          <span class="flex-1 text-left text-muted-foreground">Changes <span class="opacity-70">· {{ changedFiles.length }} {{ changedFiles.length === 1 ? "file" : "files" }}</span></span>
          <span class="text-success">+{{ totals.added }}</span>
          <span class="text-destructive">−{{ totals.removed }}</span>
        </button>
        <div v-if="open.changes" class="dc-list">
          <button v-for="f in changedFiles" :key="f.path" class="dc-sub dc-click" :title="f.path" @click="openSurface('diff')">
            <span class="min-w-0 flex-1 truncate text-left"><span class="text-foreground">{{ baseName(f.path) }}</span> <span class="text-muted-foreground">{{ dirName(f.path) }}</span></span>
            <span v-if="f.untracked" class="shrink-0 text-success">new</span>
            <template v-else>
              <span v-if="f.added" class="shrink-0 text-success">+{{ f.added }}</span>
              <span v-if="f.removed" class="shrink-0 text-destructive">−{{ f.removed }}</span>
            </template>
          </button>
        </div>
      </template>

      <!-- Running: dev servers + the chat's background tasks (collapsible) -->
      <template v-else-if="row === 'running'">
        <button class="dc-row dc-click" @click="toggleOpen('running')">
          <component :is="open.running ? PhCaretDown : PhCaretRight" :size="10" class="dc-caret" />
          <PhPlugsConnected :size="14" class="dc-icon text-success" />
          <span class="flex-1 text-left text-muted-foreground">Running</span>
          <span class="text-muted-foreground">{{ runningLabel }}</span>
        </button>
        <div v-if="open.running" class="dc-list">
          <button v-for="s in servers" :key="s.pid" class="dc-sub dc-click" :title="s.command" @click="openSurface('dev-servers')">
            <PhGlobe :size="12" class="dc-icon" />
            <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ s.command.split(" ")[0].split("/").pop() }}</span>
            <span class="shrink-0 text-muted-foreground">:{{ s.port }}</span>
          </button>
          <button v-for="t in runningTasks" :key="t.id" class="dc-sub dc-click" :title="t.command" @click="openSurface('background')">
            <PhTerminalWindow :size="12" class="dc-icon" />
            <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ t.description || t.command }}</span>
          </button>
        </div>
      </template>

      <!-- Scheduled prompts (chat) -->
      <template v-else-if="row === 'scheduled'">
        <div v-for="s in schedules" :key="s.id" class="dc-row">
          <PhClock :size="14" class="dc-icon" />
          <span class="min-w-0 flex-1 truncate text-foreground" :title="s.text">{{ s.text }}</span>
          <span class="shrink-0 text-muted-foreground" :title="s.recurs">{{ s.paused ? "paused" : s.rule?.kind && s.rule.kind !== "once" ? `${s.recurs} · ${dueLabel(s.due_at)}` : dueLabel(s.due_at) }}</span>
          <button class="dc-btn" title="Cancel" @click="cancelSchedule(s.id)"><PhX :size="11" /></button>
        </div>
      </template>

      <!-- Lineage (chat) -->
      <template v-else-if="row === 'lineage'">
        <button v-for="l in lineage" :key="l.chat.id" class="dc-row dc-click" @click="openLineage(l)">
          <component :is="l.rel === 'parent' ? PhArrowBendLeftUp : l.rel === 'side' ? PhChatsCircle : PhRobot" :size="14" class="dc-icon" />
          <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ l.chat.title || `Chat ${l.chat.id}` }}</span>
          <span class="shrink-0 text-muted-foreground">{{ REL_LABEL[l.rel] }}</span>
        </button>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, inject, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhFolder, PhFolderOpen, PhFolderNotchOpen, PhGitFork, PhCaretDown, PhCaretRight, PhCopy, PhGlobe, PhTerminalWindow, PhPlay, PhPlus, PhGitBranch,
  PhGitPullRequest, PhGitCommit, PhFileCode, PhPlugsConnected, PhClock, PhX, PhArrowBendLeftUp, PhChatsCircle, PhRobot, PhArrowUUpLeft, PhLinkBreak,
} from "@phosphor-icons/vue";
import CommitPushMenu from "./CommitPushMenu.vue";
import type { Workspace } from "@/stores/workspace";
import { useGitStore, rollupChecks, type PrChecks, type PrInfo } from "@/stores/git";
import { parsePrRef } from "@/lib/prLinkInput";
import { useScriptsStore, type Script } from "@/stores/scripts";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useClaudeChatsStore, type ClaudeSession } from "@/stores/claudeChats";
import { useUIStore } from "@/stores/ui";
import { FINDER_TARGET, lastOpenIn as lastOpenInFor, loadOpenTargets, openPathIn, openTargets } from "@/lib/openIn";
import { isWeb } from "@/lib/viewport";
import { sideChatSelection } from "@/lib/sideChat";
import { WHERE_ROWS, parseNumstat, visibleRows, type FileStat } from "@/lib/threadDetails";
import { applyBgTaskLine, emptyBgTaskState, listBgTasks, unwrapShellCommand, type BgTask, type CodexBgTerminal } from "@/lib/backgroundTasks";
import { router } from "@/router";

const props = defineProps<{ workspace: Workspace; kind: "chat" | "tab" | "workspace"; chatId?: number }>();
const emit = defineEmits<{ close: []; "open-config": []; "open-pr": [number: number, url: string] }>();

const git = useGitStore();
const scriptsStore = useScriptsStore();
const termTabs = useTerminalTabsStore();
const chats = useClaudeChatsStore();
const ui = useUIStore();
const openRightPanelSurface = inject<(surface: string, wsId: number) => void>("openRightPanelSurface")!;
const openRightPanelSubagent = inject<(chatId: number, wsId: number) => void>("openRightPanelSubagent")!;
const activeTerm = inject<() => { openClaudeChat(id: number): void } | undefined>("activeTerm")!;

const rootEl = ref<HTMLElement | null>(null);
const menu = ref<null | "ws" | "branch" | "pr">(null);
const busy = ref(false);
function toggle(m: NonNullable<typeof menu.value>) {
  menu.value = menu.value === m ? null : m;
  if (menu.value === "ws") loadOpenTargets();
  if (menu.value === "branch") void git.fetchBranches();
}

const path = computed(() => props.workspace.path);
// What the card SHOWS is read here for this card's path. The git store holds a
// single cwd several surfaces set (Right Panel, Git panel, welcome, pulse), so
// reading it hid every git row whenever it pointed elsewhere. Its actions
// (switch branch, commit/push) act on that cwd, so they show only when it's ours.
const storeHere = computed(() => git.cwd === path.value);
interface GitInfo {
  branch: string;
  upstream: boolean;
  ahead: number;
  behind: number;
  last: { shortHash: string; subject: string; author: string; relTime: string } | null;
  files: (FileStat & { untracked?: boolean })[];
}
const NO_GIT: GitInfo = { branch: "", upstream: false, ahead: 0, behind: 0, last: null, files: [] };
const gi = ref<GitInfo>(NO_GIT);
type GitOut = { code: number; stdout: string };
async function refreshGit() {
  const p = path.value;
  if (props.workspace.is_git === false) return void (gi.value = NO_GIT);
  const run = (args: string[]) => invoke<GitOut>("run_git", { cwd: p, args }).catch(() => null);
  const [head, ab, log, diff, status] = await Promise.all([
    run(["branch", "--show-current"]),
    run(["rev-list", "--left-right", "--count", "@{upstream}...HEAD"]),
    run(["log", "-1", "--pretty=format:%h%x1f%s%x1f%an%x1f%cr"]),
    run(["diff", "HEAD", "--numstat"]),
    run(["status", "--porcelain"]),
  ]);
  if (p !== path.value) return;
  if (head?.code !== 0) return void (gi.value = NO_GIT);
  const [behind, ahead] = ab?.code === 0 ? ab.stdout.trim().split(/\s+/).map((n) => parseInt(n, 10) || 0) : [0, 0];
  const [shortHash, subject, author, relTime] = log?.code === 0 && log.stdout ? log.stdout.split("\x1f") : [];
  const untracked = status?.code === 0
    ? status.stdout.split("\n").filter((l) => l.startsWith("?? ")).map((l) => ({ path: l.slice(3), added: 0, removed: 0, untracked: true }))
    : [];
  gi.value = {
    branch: head.stdout.trim() || "detached HEAD",
    upstream: ab?.code === 0,
    ahead, behind,
    last: subject ? { shortHash, subject, author, relTime } : null,
    files: [...(diff?.code === 0 ? parseNumstat(diff.stdout) : []), ...untracked],
  };
}
// The git store follows the active workspace; anything else would be a stale read.
const branch = computed(() => gi.value.branch);
const chat = computed(() => (props.kind === "chat" ? chats.sessions.find((c) => c.id === props.chatId) : undefined));
// The linked PR, re-read (state + checks) whenever the card opens on it.
const linkedPr = ref<PrInfo | null>(null);
const pr = computed<PrInfo | null>(() => {
  const c = chat.value;
  if (!c?.prNumber) return git.prByWs[props.workspace.id] ?? null;
  if (linkedPr.value?.number === c.prNumber) return linkedPr.value;
  return { number: c.prNumber, state: c.prState === "draft" ? "open" : c.prState ?? "open", isDraft: c.prState === "draft", checks: "none", url: c.prUrl ?? "" };
});
type ForgePr = { number: number; state: string; isDraft: boolean; url: string; checks?: Array<{ conclusion?: string; status?: string }> };
const toPrInfo = (p: ForgePr): PrInfo => ({ number: p.number, state: p.state, isDraft: p.isDraft, checks: rollupChecks(p.checks), url: p.url });
watch(() => chat.value?.prNumber, async (n) => {
  if (!n || !chat.value) return;
  const p = await invoke<ForgePr>("chat_pr_refresh", { chatId: chat.value.id }).catch(() => null);
  if (p && p.number === chat.value?.prNumber) linkedPr.value = toPrInfo(p);
}, { immediate: true });

// ── Open in ▾ — targets + last choice per project live in lib/openIn.
const lastOpenIn = computed(() => lastOpenInFor(path.value));
const lastOpenTarget = computed(() => openTargets.value.find((t) => t.id === lastOpenIn.value) ?? FINDER_TARGET);

async function openIn(target: string, remember = true) {
  menu.value = null;
  await openPathIn(path.value, target, remember ? { project: path.value } : {});
}
async function copyPath() {
  menu.value = null;
  await navigator.clipboard.writeText(path.value);
}

// ── Worktree moves (chat_move_to_worktree / chat_leave_worktree, chatdetails.go)
const moveBranch = ref("");
const moveError = ref("");
async function runMove(call: Promise<unknown>) {
  busy.value = true;
  moveError.value = "";
  try {
    await call;
    menu.value = null;
    moveBranch.value = "";
  } catch (e) {
    moveError.value = String(e);
  } finally {
    busy.value = false;
  }
}
function moveToWorktree() {
  const b = moveBranch.value.trim();
  if (b && chat.value) void runMove(invoke("chat_move_to_worktree", { chatId: chat.value.id, branch: b }));
}
function leaveWorktree() {
  if (chat.value) void runMove(invoke("chat_leave_worktree", { chatId: chat.value.id }));
}

// ── Link / unlink a PR (chat_link_pr / chat_unlink_pr, chatpr.go)
const prInput = ref("");
const prError = ref("");
async function linkPr() {
  if (!chat.value) return;
  if (!parsePrRef(prInput.value)) return void (prError.value = "Enter #123 or a pull request URL");
  busy.value = true;
  prError.value = "";
  try {
    linkedPr.value = toPrInfo(await invoke<ForgePr>("chat_link_pr", { chatId: chat.value.id, ref: prInput.value.trim() }));
    prInput.value = "";
    menu.value = null;
  } catch (e) {
    prError.value = String(e);
  } finally {
    busy.value = false;
  }
}
async function unlinkPr() {
  if (!chat.value) return;
  await invoke("chat_unlink_pr", { chatId: chat.value.id }).catch((e) => (prError.value = String(e)));
  linkedPr.value = null;
  menu.value = null;
}

// ── Branch
const branchError = ref("");
async function switchBranch(b: string) {
  branchError.value = "";
  try {
    await git.switchBranch(b);
    menu.value = null;
  } catch (e) {
    branchError.value = String(e);
  }
}

const CHECK_LABEL: Record<PrChecks, string> = { pass: "checks pass", fail: "checks failing", pending: "checks running", none: "" };
const CHECK_CLASS: Record<PrChecks, string> = { pass: "text-success", fail: "text-destructive", pending: "text-muted-foreground", none: "" };

// ── Scripts
const scripts = computed(() => scriptsStore.scriptsFor(path.value));
watch(path, (p) => void scriptsStore.loadForPath(p), { immediate: true });
function runScript(sc: Script) {
  const cmd = scriptsStore.commandLine(sc);
  if (!cmd) return;
  ui.closeWelcome();
  termTabs.add(props.workspace.id, cmd);
}

function openSurface(id: string) {
  openRightPanelSurface(id, props.workspace.id);
}

// ── Polled rows: changes, dev servers, schedules. Only while the card is up.
const changedFiles = computed(() => gi.value.files);
const totals = computed(() => changedFiles.value.reduce((t, f) => ({ added: t.added + f.added, removed: t.removed + f.removed }), { added: 0, removed: 0 }));
const baseName = (p: string) => p.split("/").pop() ?? p;
const dirName = (p: string) => p.split("/").slice(0, -1).join("/");

// Collapsed/expanded per section, one setting for every project.
const OPEN_KEY = "burrow.detailsOpen";
const open = ref<{ changes: boolean; running: boolean }>({ changes: false, running: false, ...JSON.parse(localStorage.getItem(OPEN_KEY) ?? "{}") });
function toggleOpen(k: "changes" | "running") {
  open.value[k] = !open.value[k];
  localStorage.setItem(OPEN_KEY, JSON.stringify(open.value));
}
interface DevServer { pid: number; port: number; command: string }
const servers = ref<DevServer[]>([]);
interface Scheduled { id: number; text: string; due_at: string; recurs: string; paused: boolean; rule?: { kind: string } }
const schedules = ref<Scheduled[]>([]);

// The chat's background tasks, folded incrementally from its stream like the
// Right Panel's Background surface (BackgroundTasksSurface.vue); only running ones show.
let bgState = emptyBgTaskState();
let bgChat: number | undefined;
const runningTasks = ref<BgTask[]>([]);
async function refreshTasks(id: number) {
  if (bgChat !== id) { bgChat = id; bgState = emptyBgTaskState(); }
  const [lines, live] = await Promise.all([
    invoke<{ ord: number; line: string }[]>("load_chat_task_lines_since", { chatId: id, since: bgState.lastOrd + 1 }).catch(() => []),
    invoke<CodexBgTerminal[]>("chat_list_bg_terminals", { chatId: id }).catch(() => []),
  ]);
  if (bgChat !== id) return;
  for (const l of lines) applyBgTaskLine(bgState, l.line, l.ord);
  runningTasks.value = [
    ...listBgTasks(bgState).filter((t) => t.status === "running"),
    ...live.map((t) => ({ id: t.processId, description: "", command: unwrapShellCommand(t.command) } as BgTask)),
  ];
}
const runningLabel = computed(() => [
  servers.value.length && `${servers.value.length} ${servers.value.length === 1 ? "server" : "servers"}`,
  runningTasks.value.length && `${runningTasks.value.length} ${runningTasks.value.length === 1 ? "task" : "tasks"}`,
].filter(Boolean).join(" · "));

async function refreshPolled() {
  const p = path.value;
  const id = chat.value?.id;
  void refreshGit();
  if (id) void refreshTasks(id);
  else runningTasks.value = [];
  const [srv, sched] = await Promise.all([
    invoke<DevServer[]>("list_dev_servers", { workspacePath: p }).catch(() => []),
    id ? invoke<Scheduled[]>("chat_schedule_list", { chatId: id }).catch(() => []) : [],
  ]);
  if (p !== path.value) return;
  servers.value = srv;
  schedules.value = sched;
}
async function cancelSchedule(id: number) {
  await invoke("chat_schedule_cancel", { id }).catch((e) => console.error("chat_schedule_cancel failed", e));
  void refreshPolled();
}
function dueLabel(iso: string): string {
  const d = new Date(iso);
  return d.toDateString() === new Date().toDateString()
    ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : d.toLocaleDateString([], { month: "short", day: "numeric" });
}

// The git store's lists change whenever it re-reads status: recount then.
// Re-read on open, every poll, and whenever the store re-reads status for this path (a commit, a stage).
watch(() => [path.value, storeHere.value && [git.branch, git.staged, git.unstaged, git.untracked, git.ahead]], refreshGit, { immediate: true });
watch(() => [path.value, props.chatId], refreshPolled, { immediate: true });
let timer: number | undefined;
onMounted(() => { timer = window.setInterval(refreshPolled, 5000); });

// ── Lineage: parent, side chats, sub-agents.
type Rel = "parent" | "side" | "subagent";
const REL_LABEL: Record<Rel, string> = { parent: "parent", side: "side chat", subagent: "sub-agent" };
const lineage = computed<{ rel: Rel; chat: ClaudeSession }[]>(() => {
  const c = chat.value;
  if (!c) return [];
  const parent = c.parentChatId ? chats.sessions.find((s) => s.id === c.parentChatId) : undefined;
  const kids = chats.childrenOf(c.id).filter((s) => !s.archivedAt);
  return [
    ...(parent ? [{ rel: "parent" as const, chat: parent }] : []),
    ...kids.map((s) => ({ rel: (s.kind === "side" ? "side" : "subagent") as Rel, chat: s })),
  ];
});
function openLineage(l: { rel: Rel; chat: ClaudeSession }) {
  if (l.rel === "subagent") return openRightPanelSubagent(l.chat.id, l.chat.workspaceId);
  if (l.rel === "side") {
    sideChatSelection[chat.value!.id] = l.chat.id;
    return openSurface("side-chat");
  }
  const tab = termTabs.tabsByWs[l.chat.workspaceId]?.find((t) => t.chatId === l.chat.id);
  if (tab) void router.push(`/ws/${l.chat.workspaceId}/tab/${tab.id}`);
  else activeTerm()?.openClaudeChat(l.chat.id);
}

const rows = computed(() => visibleRows({
  kind: props.kind,
  branch: branch.value,
  hasPr: !!pr.value,
  hasLinkedPr: !!chat.value?.prNumber,
  changedFiles: changedFiles.value.length,
  running: servers.value.length + runningTasks.value.length,
  scheduled: schedules.value.length,
  lineage: lineage.value.length,
}));
const firstGitRow = computed(() => rows.value.find((r) => !WHERE_ROWS.includes(r)));

// Esc closes the card — unless it belongs to something else that has focus
// (a chat's Esc interrupts the turn, a terminal's goes to the PTY).
function onKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  if (menu.value) return void (menu.value = null);
  const focus = document.activeElement;
  if (!focus || focus === document.body || rootEl.value?.contains(focus)) emit("close");
}
function onDocClick() { menu.value = null; }
onMounted(() => {
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("click", onDocClick);
});
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("click", onDocClick);
  clearInterval(timer);
});
</script>

<style scoped>
.details-card {
  background: var(--bg-dropdown, var(--bg-panel));
  backdrop-filter: var(--blur-dropdown, blur(18px)) saturate(140%);
  -webkit-backdrop-filter: var(--blur-dropdown, blur(18px)) saturate(140%);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
}
.dc-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 2px 8px 2px 10px;
  width: 100%;
}
.dc-click { cursor: pointer; }
.dc-click:hover { background: var(--bg-hover); }
.dc-icon { flex-shrink: 0; color: var(--text-muted); }
.dc-caret { flex-shrink: 0; margin-left: -6px; margin-right: -2px; color: var(--text-muted); }
.dc-list { max-height: 180px; overflow-y: auto; }
.dc-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-height: 24px;
  padding: 1px 8px 1px 30px;
  font-size: 11.5px;
}
.dc-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 5px;
  border-radius: 5px;
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}
.dc-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.dc-menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  min-width: 190px;
  background: var(--bg-dropdown, var(--bg-panel));
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px;
  z-index: 1000;
}
.dc-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  border-radius: 4px;
  color: var(--text-secondary);
  font-size: 12px;
  text-align: left;
  white-space: nowrap;
}
.dc-menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.dc-menu-item:disabled { opacity: 0.4; cursor: default; }
.dc-input {
  width: 100%;
  padding: 4px 6px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: transparent;
  color: var(--text-primary);
  font-size: 12px;
  outline: none;
}
.dc-input:focus { border-color: var(--color-accent); }
</style>
