<template>
  <!-- Collapsed: round launcher button, bottom-right -->
  <button
    v-if="!ui.floatChatOpen"
    class="fc-launcher fixed bottom-[18px] right-[18px] z-[60] flex h-[46px] w-[46px] items-center justify-center rounded-full border border-border text-muted-foreground shadow-[0_6px_20px_rgba(0,0,0,0.35)] transition-[transform,box-shadow,border-color,color] duration-150 ease-out hover:-translate-y-0.5 hover:border-accent hover:shadow-[0_10px_26px_rgba(0,0,0,0.42)]"
    :class="{ 'fc-busy': busy, 'border-accent': needsAttention }"
    :title="launcherTitle"
    @click="open"
  >
    <PhSparkle :size="20" weight="fill" class="text-accent" />
    <span v-if="busy" class="absolute right-[5px] top-[5px] h-[9px] w-[9px] rounded-full border-2 border-panel bg-green-400" />
    <span
      v-if="needsAttention"
      class="fc-badge absolute left-1 top-1 h-3 w-3 rounded-full border-2 border-panel shadow-[0_0_0_1px_rgba(0,0,0,0.3)]"
      :class="{
        'bg-amber-500 fc-badge-permission': attentionKind === 'permission',
        'bg-blue-500': attentionKind === 'waiting',
        'bg-green-500': attentionKind === 'done',
      }"
    />
  </button>

  <!-- Expanded: compact chat card -->
  <div v-else class="fc-card fixed bottom-[18px] right-[18px] z-[60] flex h-[540px] w-[460px] max-h-[calc(100vh-64px)] flex-col overflow-hidden rounded-[14px] border border-border shadow-[0_12px_40px_rgba(0,0,0,0.5)]">
    <div class="fc-solid flex shrink-0 items-center gap-1.5 border-b border-border px-2.5 py-2">
      <PhSparkle :size="14" weight="fill" class="shrink-0 text-accent" />
      <span class="text-xs font-semibold text-foreground">Manager</span>
      <span class="flex-1 truncate text-[10px] text-muted-foreground" :title="rootCwd">{{ rootName }}</span>
      <button
        class="flex h-[22px] w-[22px] items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground"
        :class="{ 'bg-accent/14 text-accent hover:text-accent': worktreeMode }"
        :title="worktreeMode
          ? 'Spawn mode: worktree per agent (isolated) — click for active branch'
          : 'Spawn mode: active branch (shared) — click for worktree per agent'"
        @click="toggleWorktreeMode"
      >
        <PhTree v-if="worktreeMode" :size="13" weight="bold" />
        <PhGitBranch v-else :size="13" weight="bold" />
      </button>
      <button class="flex h-[22px] w-[22px] items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="Reset session (clears history, starts fresh)" @click="resetSession">
        <PhArrowCounterClockwise :size="13" weight="bold" />
      </button>
      <button class="flex h-[22px] w-[22px] items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="Collapse" @click="ui.toggleFloatChat()">
        <PhMinus :size="13" weight="bold" />
      </button>
    </div>
    <div class="fc-solid fc-body min-h-0 flex-1">
      <AgentChat
        v-if="controlChatId !== null"
        :key="controlChatId"
        compact
        :chat-id="controlChatId"
        :workspace-id="rootId"
        :cwd="rootCwd"
        :append-system-prompt="managerPrimer"
        :avatar-src="managerAvatar"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhSparkle, PhMinus, PhGitBranch, PhTree, PhArrowCounterClockwise } from "@phosphor-icons/vue";
import { getDefaultManagerPrimer, SPAWN_MODE_WORKTREE, SPAWN_MODE_BRANCH } from "@/utils/managerPrimer";
import AgentChat from "./AgentChat.vue";
import { useUIStore } from "@/stores/ui";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import managerAvatar from "@/assets/manager-avatar.png";

const props = defineProps<{ cwd: string; wsId: number }>();

const ui = useUIStore();
const chats = useClaudeChatsStore();
const wsStore = useWorkspaceStore();

// activeWsId tracks which workspace the Manager is currently anchored to.
// It only updates when the Manager session is idle so that switching workspaces
// while a task is in progress doesn't interrupt the running claude process.
const activeWsId = ref<number>(props.wsId);
const activeCwd = ref<string>(props.cwd);

// The Manager is anchored to the ROOT repo, not the active worktree. Worktrees
// are their own workspace rows (parent_id set); keying the session by the root id
// keeps the same Manager session alive as you switch between a repo's worktrees,
// instead of showing an empty one per worktree.
const root = computed(() => {
  const w = wsStore.workspaces.find((x) => x.id === activeWsId.value);
  if (w?.parent_id) {
    const parent = wsStore.workspaces.find((x) => x.id === w.parent_id);
    if (parent) return parent;
  }
  return w ?? null;
});
const rootId = computed(() => root.value?.id ?? activeWsId.value);
const rootCwd = computed(() => root.value?.path ?? activeCwd.value);
const rootName = computed(() => root.value?.name ?? "this repo");

// One persistent Manager session per ROOT repo, reused across open/collapse,
// worktree switches, and app restarts. Keyed by root repo id in localStorage.
const MAP_KEY = "burrow.floatchat.sessions";
function loadMap(): Record<number, number> {
  try { return JSON.parse(localStorage.getItem(MAP_KEY) || "{}"); } catch { return {}; }
}
function saveMap(m: Record<number, number>) {
  localStorage.setItem(MAP_KEY, JSON.stringify(m));
}

const controlChatId = ref<number | null>(null);

// Worktree preference: false = spawn agents in the repo's active branch (no
// worktree, default), true = isolate each spawned agent in its own git worktree.
// Persisted globally; the Manager primer reflects the current choice each turn.
const WT_KEY = "burrow.floatchat.worktreeMode";
const worktreeMode = ref<boolean>(localStorage.getItem(WT_KEY) === "1");
watch(worktreeMode, (v) => localStorage.setItem(WT_KEY, v ? "1" : "0"));
function toggleWorktreeMode() { worktreeMode.value = !worktreeMode.value; }

// When the active workspace changes, adopt it only if the Manager is idle.
// If a task is running, defer until it finishes so we don't kill claude mid-turn.
watch(
  () => [props.wsId, props.cwd] as const,
  ([wsId, cwd]) => {
    const busy = controlChatId.value
      ? chats.sessions.find((s) => s.id === controlChatId.value)?.busy
      : false;
    if (!busy) {
      activeWsId.value = wsId;
      activeCwd.value = cwd;
    }
  },
);

function ensureControlSession(repoId: number) {
  const map = loadMap();
  const existing = map[repoId];
  if (existing && chats.sessions.find((s) => s.id === existing)) {
    controlChatId.value = existing;
    return;
  }
  // create() flips the workspace's active chat; restore it so the in-tab Claude
  // pane isn't yanked to this hidden Manager session.
  const prevActive = chats.activeByWs[repoId];
  const sess = chats.create(repoId);
  chats.sync(sess.id, { title: "Manager", control: true });
  if (prevActive) chats.setActive(repoId, prevActive);
  map[repoId] = sess.id;
  saveMap(map);
  controlChatId.value = sess.id;
}

// Resolve the Manager session lazily — only once the card is first opened for a
// repo, so we don't spawn a `claude` process for users who never use it.
watch(
  () => [ui.floatChatOpen, rootId.value] as const,
  ([isOpen, repoId]) => {
    if (isOpen && typeof repoId === "number") ensureControlSession(repoId);
  },
  { immediate: true },
);

function open() {
  ui.floatChatOpen = true;
  finishedWhileCollapsed.value = false;
}

// The live Manager session row (status/busy mirror the in-tab chat model).
const session = computed(() =>
  controlChatId.value === null
    ? null
    : chats.sessions.find((s) => s.id === controlChatId.value) ?? null,
);

// Busy dot: Manager session's agent is mid-turn.
const busy = computed(() => !!session.value?.busy);

// Latch a turn that completed while the card was collapsed, so the user gets a
// "finished while you were away" badge even though chat status falls back to idle.
const finishedWhileCollapsed = ref(false);
watch(
  () => session.value?.busy,
  (now, prev) => {
    if (prev && !now && !ui.floatChatOpen) finishedWhileCollapsed.value = true;
    // Adopt a deferred workspace switch that was blocked by an in-progress task.
    if (prev && !now) {
      activeWsId.value = props.wsId;
      activeCwd.value = props.cwd;
    }
  },
);

// Attention badge: blocked on input (permission/waiting) or finished while away.
// Permission outranks waiting outranks a plain finish.
const attentionKind = computed<"permission" | "waiting" | "done" | null>(() => {
  const st = session.value?.status;
  if (st === "permission") return "permission";
  if (st === "waiting") return "waiting";
  if (finishedWhileCollapsed.value) return "done";
  return null;
});
const needsAttention = computed(() => attentionKind.value !== null);

const launcherTitle = computed(() => {
  switch (attentionKind.value) {
    case "permission": return "Manager needs a permission decision";
    case "waiting": return "Manager is waiting for your input";
    case "done": return "Manager finished while you were away";
    default: return "Manager — orchestrate worktrees, agents & PRs with chat";
  }
});

function resetSession() {
  const repoId = rootId.value;
  if (typeof repoId !== "number") return;
  const map = loadMap();
  delete map[repoId];
  saveMap(map);
  controlChatId.value = null;
  ensureControlSession(repoId);
}

onMounted(() => {
  if (ui.floatChatOpen && typeof rootId.value === "number") ensureControlSession(rootId.value);
});

const projectManagerPrompt = ref('');
watch(rootCwd, async (cwd) => {
  if (!cwd) return;
  try {
    const content = await invoke<string>('read_text_file', { path: cwd + '/.burrow/manager.md' });
    const stripped = content.replace(/<!--[\s\S]*?-->/g, '').trim();
    const isPlaceholder = stripped === '# Project-specific Manager instructions' || stripped === '';
    projectManagerPrompt.value = isPlaceholder ? '' : stripped;
  } catch {
    projectManagerPrompt.value = '';
  }
}, { immediate: true });

const managerPrimer = computed(() => {
  if (projectManagerPrompt.value) {
    const spawnMode = worktreeMode.value ? SPAWN_MODE_WORKTREE : SPAWN_MODE_BRANCH;
    return projectManagerPrompt.value + '\n\n---\n\n' + spawnMode;
  }
  return getDefaultManagerPrimer(worktreeMode.value);
});
</script>

<style scoped>
/* Force a SOLID surface even under translucent themes (e.g. "stonks", whose
   --bg-panel is rgba): composite the panel tint over an opaque --bg-base.
   Tailwind's bg-* utilities can't express a two-layer gradient composite. */
.fc-solid {
  background-color: var(--bg-base, #0d0d0d);
  background-image: linear-gradient(var(--bg-panel, #111111), var(--bg-panel, #111111));
  backdrop-filter: none;
}
.fc-launcher, .fc-card { background-color: var(--bg-base, #0d0d0d); background-image: linear-gradient(var(--bg-panel, #111111), var(--bg-panel, #111111)); backdrop-filter: none; }

.fc-launcher.fc-busy { animation: fc-pulse 1.4s ease-in-out infinite; }
.fc-badge-permission { animation: fc-badge-pulse 1.2s ease-in-out infinite; }
@keyframes fc-badge-pulse {
  0%, 100% { box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.3); }
  50% { box-shadow: 0 0 0 4px rgba(245, 158, 11, 0.35); }
}
@keyframes fc-pulse {
  0%, 100% { box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35); }
  50% { box-shadow: 0 6px 26px var(--accent, #7c5cff); }
}

/* Kill any per-theme translucency on the embedded chat so the card stays solid. */
.fc-body :deep(.claude-chat) { background: transparent; backdrop-filter: none; }
</style>
