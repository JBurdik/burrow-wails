<template>
  <div class="flex min-h-0 flex-1 flex-col bg-base">
    <!-- Header: what this Manager is anchored to, and the few knobs it needs -->
    <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
      <PhSparkle :size="14" weight="fill" class="shrink-0 text-accent" />
      <span class="shrink-0 text-[11px] font-semibold text-foreground">Manager</span>
      <span
        class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-muted-foreground"
        :title="rootCwd"
      >{{ rootName }}</span>

      <Select
        v-model="newThreadAgent"
        :options="agentOptions"
        class="h-6 max-w-[110px] rounded border border-border bg-hover px-1 text-[10px] text-foreground outline-none hover:border-muted-foreground"
        title="Agent used for the next Manager thread"
      />

      <button
        class="flex h-6 items-center gap-1 rounded border px-1.5 text-[10px] transition-colors"
        :class="worktreeMode
          ? 'border-accent/50 bg-accent/15 text-accent'
          : 'border-border bg-hover text-muted-foreground hover:text-foreground'"
        :title="worktreeMode
          ? 'Each sub-agent gets its own git worktree'
          : 'Sub-agents run on the active branch'"
        @click="worktreeMode = !worktreeMode"
      >
        <PhGitBranch :size="11" />
        {{ worktreeMode ? "Isolated" : "Shared" }}
      </button>

      <button
        class="flex h-6 w-6 items-center justify-center rounded border border-border bg-hover text-muted-foreground transition-colors hover:text-foreground"
        title="Start a fresh Manager thread for this repo"
        @click="resetThread"
      >
        <PhPlus :size="11" />
      </button>
    </div>

    <!-- One chat per repo the user has engaged, kept mounted and toggled with
         v-show: unmounting would stop the agent process, so a Manager that's
         mid-task keeps working while you look at another project. -->
    <div class="relative min-h-0 flex-1">
      <AgentChat
        v-for="thread in threads"
        v-show="thread.repoId === rootId"
        :key="thread.sessionId"
        compact
        model-key="burrow.manager.model"
        :default-model="DEFAULT_MANAGER_MODEL"
        :chat-id="thread.sessionId"
        :workspace-id="thread.repoId"
        :cwd="thread.cwd"
        :agent-kind="thread.agentKind"
        :append-system-prompt="primer"
      />
      <div
        v-if="!threads.length"
        class="flex h-full items-center justify-center px-6 text-center text-[11px] leading-relaxed text-muted-foreground"
      >
        Loading Manager…
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The Manager: a per-repository orchestrator chat.
 *
 * Its capabilities are Burrow's control verbs (see internal/control), which it
 * reaches as MCP tools or via the `burrow` CLI depending on the agent running
 * it. This component only has to do three things: keep one thread per ROOT repo
 * alive, hand that thread the generated primer, and stay out of the way — the
 * message stream, composer, permission gates and model picker all come from
 * AgentChat.
 */
import { ref, computed, watch, onMounted } from "vue";
import { PhSparkle, PhGitBranch, PhPlus } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import AgentChat from "./AgentChat.vue";
import { Select } from "@/components/ui/select";
import { useWorkspaceStore } from "@/stores/workspace";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useProvidersStore } from "@/stores/providers";
import { useUIStore } from "@/stores/ui";
import { getConfig, setConfig, configReady, migrateFromLocalStorage } from "@/lib/config";
import { buildManagerPrimer, fetchControlVerbs, type ControlVerb } from "@/utils/managerPrimer";

const props = defineProps<{ cwd: string; wsId: number }>();

const wsStore = useWorkspaceStore();
const chats = useClaudeChatsStore();
const providers = useProvidersStore();
const ui = useUIStore();

const DEFAULT_MANAGER_MODEL = "claude-sonnet-5";
const THREADS_KEY = "managerThreads";
const WORKTREE_KEY = "managerWorktreeMode";
const AGENT_KEY = "managerAgent";

// ── Anchor: the ROOT repo ────────────────────────────────────────────────────
// A worktree is its own workspace row (parent_id set), so keying by the root
// keeps ONE Manager per project instead of one per branch — it stays useful
// while the user hops between a repo and its worktrees.
const root = computed(() => {
  const self = wsStore.workspaces.find((w) => w.id === props.wsId);
  if (!self) return undefined;
  return self.parent_id ? wsStore.workspaces.find((w) => w.id === self.parent_id) ?? self : self;
});
const rootId = computed(() => root.value?.id ?? props.wsId);
const rootCwd = computed(() => root.value?.path ?? props.cwd);
const rootName = computed(() => root.value?.name ?? "this repo");

// ── Threads: one session per engaged repo ────────────────────────────────────
type Thread = { repoId: number; sessionId: number; cwd: string; agentKind: string };
const threads = ref<Thread[]>([]);

const agents = computed(() => providers.chatAgents);
const agentOptions = computed(() => agents.value.map((agent) => ({ value: agent.id, label: agent.name })));
const newThreadAgent = ref("claude");
watch(newThreadAgent, (v) => setConfig(AGENT_KEY, v));

const worktreeMode = ref(false);
watch(worktreeMode, (v) => setConfig(WORKTREE_KEY, v));

/** Persisted repo → session id, so a Manager thread survives a restart. */
function savedSessions(): Record<number, number> {
  return getConfig<Record<number, number>>(THREADS_KEY, {});
}

function rememberSession(repoId: number, sessionId: number) {
  setConfig(THREADS_KEY, { ...savedSessions(), [repoId]: sessionId });
}

/**
 * Make sure the current root repo has a live Manager thread, reusing the
 * persisted one when its session still exists. The session is flagged
 * `control` so it stays out of the Sidebar's chat list — the Manager has its
 * own home in the right panel.
 */
function ensureThread() {
  const repoId = rootId.value;
  if (!repoId) return;
  if (threads.value.some((t) => t.repoId === repoId)) return;

  const savedId = savedSessions()[repoId];
  const existing = savedId != null ? chats.sessions.find((s) => s.id === savedId) : undefined;
  if (existing) {
    threads.value.push({
      repoId,
      sessionId: existing.id,
      cwd: rootCwd.value,
      agentKind: existing.agentKind ?? "claude",
    });
    return;
  }

  // create() makes the new chat that workspace's active one; the Manager is a
  // side panel, not the workspace's chat, so put the previous one back.
  const previousActive = chats.activeByWs[repoId];
  const session = chats.create(repoId, { agentKind: newThreadAgent.value });
  chats.sync(session.id, { title: "Manager", control: true });
  if (previousActive) chats.setActive(repoId, previousActive);

  rememberSession(repoId, session.id);
  threads.value.push({ repoId, sessionId: session.id, cwd: rootCwd.value, agentKind: newThreadAgent.value });
}

async function resetThread() {
  const repoId = rootId.value;
  const current = threads.value.find((t) => t.repoId === repoId);
  threads.value = threads.value.filter((t) => t.repoId !== repoId);
  if (current) await chats.remove(current.sessionId);
  ensureThread();
}

watch(rootId, ensureThread);

// ── Primer ───────────────────────────────────────────────────────────────────
// Verbs come from the running app's registry, so the Manager is described in
// terms of what this build can actually do.
const verbs = ref<ControlVerb[]>([]);
const projectPrompt = ref("");

const primer = computed(() =>
  buildManagerPrimer(verbs.value, {
    worktreeMode: worktreeMode.value,
    repoName: rootName.value,
    projectPrompt: projectPrompt.value,
  }),
);

/** Project-specific Manager instructions from <repo>/.burrow/manager.md. */
async function loadProjectPrompt(cwd: string) {
  if (!cwd) {
    projectPrompt.value = "";
    return;
  }
  try {
    const content = await invoke<string>("read_text_file", { path: `${cwd}/.burrow/manager.md` });
    const stripped = content.replace(/<!--[\s\S]*?-->/g, "").trim();
    projectPrompt.value = stripped === "# Project-specific Manager instructions" ? "" : stripped;
  } catch {
    projectPrompt.value = "";
  }
}

watch(rootCwd, loadProjectPrompt);

onMounted(async () => {
  await configReady;
  migrateFromLocalStorage("burrow.floatchat.sessions", THREADS_KEY);
  migrateFromLocalStorage("burrow.floatchat.worktreeMode", WORKTREE_KEY);
  worktreeMode.value = getConfig(WORKTREE_KEY, false);
  newThreadAgent.value = getConfig(AGENT_KEY, ui.defaultChatAgent || "claude");

  verbs.value = await fetchControlVerbs();
  await loadProjectPrompt(rootCwd.value);
  ensureThread();
});
</script>
