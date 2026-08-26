<template>
  <aside class="flex w-[var(--sidebar-width,220px)] shrink-0 grow-0 basis-[var(--sidebar-width,220px)] flex-col overflow-hidden border-r border-border bg-panel [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)]">
    <!-- Project filter: "All projects" or one repo, filters the groups below -->
    <div class="shrink-0 p-1.5" ref="filterEl">
      <button
        class="flex w-full items-center gap-[7px] rounded-[7px] border border-border bg-base px-2 py-1.5 text-left transition-colors hover:bg-hover"
        :class="filterOpen && 'bg-hover border-[color-mix(in_srgb,var(--accent)_40%,var(--border))]'"
        @click.stop="filterOpen = !filterOpen"
      >
        <PhFolder :size="13" weight="fill" class="shrink-0 text-accent" />
        <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-foreground">{{ filterProjectId == null ? "All projects" : (repoById(filterProjectId)?.name ?? "All projects") }}</span>
        <PhCaretDown :size="11" weight="bold" class="shrink-0 text-muted-foreground" />
      </button>
      <Teleport to="body">
        <div v-if="filterOpen" class="fixed z-[1000] max-h-[60vh] overflow-y-auto rounded-lg border border-border bg-panel p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="filterMenuStyle" @click.stop>
          <button
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="filterProjectId == null && 'bg-[color-mix(in_srgb,var(--accent)_10%,transparent)] text-foreground'"
            @click="filterProjectId = null; filterOpen = false"
          >
            All projects
          </button>
          <button
            v-for="repo in store.topLevel"
            :key="repo.id"
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="filterProjectId === repo.id && 'bg-[color-mix(in_srgb,var(--accent)_10%,transparent)] text-foreground'"
            @click="filterProjectId = repo.id; filterOpen = false"
          >
            <img v-if="store.icons[repo.id]" :src="store.icons[repo.id]" class="h-3.5 w-3.5 shrink-0 rounded-sm object-cover" />
            <PhFolder v-else :size="12" weight="fill" class="shrink-0 text-accent" />
            {{ repo.name }}
          </button>
        </div>
      </Teleport>
    </div>

    <!-- Grouped-by-project list: one group per repo, one row per workspace/worktree -->
    <div class="flex-1 overflow-y-auto py-0.5 pb-1.5">
      <div v-for="group in groups" :key="group.repo.id" class="group/proj mb-1.5">
        <div class="mx-1 flex items-center gap-1.5 px-2 py-1">
          <img v-if="store.icons[group.repo.id]" :src="store.icons[group.repo.id]" class="h-[15px] w-[15px] shrink-0 rounded object-cover" />
          <PhFolder v-else :size="13" weight="fill" class="shrink-0 text-accent" />
          <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-bold text-foreground">{{ group.repo.name }}</span>
          <button
            class="flex items-center rounded-md p-1 text-muted-foreground opacity-0 transition-colors hover:bg-hover hover:text-foreground group-hover/proj:opacity-100 active:scale-90"
            :class="isPinned(group.repo.id) && '!opacity-100 !text-accent'"
            :title="isPinned(group.repo.id) ? 'Unpin' : 'Pin to top'"
            @click.stop="togglePin(group.repo.id)"
          ><PhPushPin :size="11" :weight="isPinned(group.repo.id) ? 'fill' : 'regular'" /></button>
          <button class="flex items-center rounded-md p-1 text-muted-foreground opacity-0 transition-colors hover:bg-hover hover:text-foreground group-hover/proj:opacity-100 active:scale-90" title="New worktree" @click.stop="openWtDialog(group.repo)"><PhGitBranch :size="12" /></button>
        </div>

        <div v-for="ws in group.rows" :key="ws.id" class="mb-1">
          <div class="tabs-header group/header mx-1 flex cursor-pointer items-center gap-[5px] rounded-md py-[5px] pb-1 pl-2.5 pr-2 transition-colors hover:bg-hover" :class="active?.id === ws.id && 'active'" @click="selectWs(ws)" @contextmenu.prevent.stop="ws.parent_id == null && startRename(ws)">
            <PhGitBranch v-if="ws.parent_id" :size="11" class="shrink-0 text-[#a78bfa]" />
            <span class="tabs-label overflow-hidden text-ellipsis whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.07em] text-muted-foreground">{{ ws.worktree_branch || ws.name }}</span>
            <span v-if="tabCount(ws.id)" class="rounded-lg bg-hover px-1.5 py-px text-[9px] font-semibold leading-[1.6] text-muted-foreground">{{ tabCount(ws.id) }}</span>
            <span
              v-if="git.prByWs[ws.id]"
              class="flex shrink-0 items-center gap-[3px] rounded-[7px] bg-white/[0.06] px-[5px] py-px pl-1 font-mono text-[9px] font-semibold leading-none text-muted-foreground"
              :class="{
                'text-[#4ade80] bg-[color-mix(in_srgb,#4ade80_12%,transparent)]': prTone(git.prByWs[ws.id]!) === 'open',
                'text-[#9ca3af]': prTone(git.prByWs[ws.id]!) === 'draft',
                'text-[#a78bfa] bg-[color-mix(in_srgb,#a78bfa_14%,transparent)]': prTone(git.prByWs[ws.id]!) === 'merged',
                'text-[#f87171] bg-[color-mix(in_srgb,#f87171_12%,transparent)]': prTone(git.prByWs[ws.id]!) === 'closed',
                'text-[#f87171] bg-[color-mix(in_srgb,#f87171_14%,transparent)]': prTone(git.prByWs[ws.id]!) === 'fail',
                'text-[#fbbf24] bg-[color-mix(in_srgb,#fbbf24_14%,transparent)]': prTone(git.prByWs[ws.id]!) === 'pending',
              }"
              :title="prTitle(git.prByWs[ws.id]!)"
            ><span
              class="h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground"
              :class="{
                'bg-[#4ade80]': prTone(git.prByWs[ws.id]!) === 'open',
                'bg-[#9ca3af]': prTone(git.prByWs[ws.id]!) === 'draft',
                'bg-[#a78bfa]': prTone(git.prByWs[ws.id]!) === 'merged',
                'bg-[#f87171]': prTone(git.prByWs[ws.id]!) === 'closed' || prTone(git.prByWs[ws.id]!) === 'fail',
                'bg-[#fbbf24] animate-[pr-pulse_1.6s_ease-in-out_infinite]': prTone(git.prByWs[ws.id]!) === 'pending',
                'animate-[pr-pulse_1.6s_ease-in-out_infinite]': prTone(git.prByWs[ws.id]!) === 'fail',
              }"
            />#{{ git.prByWs[ws.id]!.number }}</span>
            <span v-if="rowAggStatus(ws.id)" class="status-dot" :class="`status-${rowAggStatus(ws.id)}`">{{ rowAggStatus(ws.id) === 'running' ? spinnerFrame : '' }}</span>
            <div class="ml-auto flex items-center gap-px opacity-0 transition-opacity group-hover/header:opacity-100" :class="active?.id === ws.id && '!opacity-100'">
              <button class="flex items-center rounded-md p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="Board" @click.stop="ui.openBoard(ws.id)"><PhKanban :size="13" /></button>
              <button class="flex items-center rounded-md p-1 text-[#d97706] transition-colors hover:bg-[#d97706]/[0.14] active:scale-90" title="New conversation" aria-label="New conversation" @click.stop="newChatSession(ws)"><PhChatCenteredText :size="13" /></button>
              <button class="flex items-center rounded-md p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="New terminal" @click.stop="newTerminalTab(ws)"><PhPlus :size="13" /></button>
            </div>
          </div>

          <template v-if="active?.id === ws.id">
            <TransitionGroup name="ws-move" tag="div" class="ws-terminals ml-3.5 mb-[3px] mr-1.5 mt-px flex flex-col gap-px border-l border-border/55 pl-1.5">
              <div
                v-for="(tab, tabIdx) in termTabs.tabsByWs[ws.id] || []"
                :key="tab.id"
                class="ws-term group relative flex touch-none items-center gap-1.5 rounded-md px-[7px] py-[5px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
                :data-reorder-idx="tabIdx"
                :data-reorder-group="String(ws.id)"
                :class="{
                  active: termTabs.activeByWs[ws.id] === tab.id,
                  [`agent-state-${attentionState(ws.id, tab.id, tab.status)}`]: tab.isAgent,
                  'drag-over outline outline-1 outline-accent -outline-offset-1 bg-hover': tabDragGroup === String(ws.id) && tabOverIdx === tabIdx && tabDragIdx !== tabIdx,
                  'dragging opacity-40': tabDragGroup === String(ws.id) && tabDragIdx === tabIdx,
                }"
                @click.stop="selectTab(ws, tab.id)"
                @dblclick.stop
                @pointerdown="(e: PointerEvent) => tabDragDown(tabIdx, e, String(ws.id))"
              >
                <PhChatCenteredText v-if="tab.isChat" :size="11" class="claude-session-icon shrink-0 text-[#d97706]" :class="termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
                <PhRobot v-else-if="tab.isAgent" :size="11" class="ws-term-icon-agent shrink-0 text-accent" :class="termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
                <PhTerminal v-else :size="11" class="shrink-0 text-muted-foreground" :class="termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
                <input
                  v-if="editingTab?.wsId === ws.id && editingTab?.tabId === tab.id"
                  v-model="editingTabTitle"
                  class="ws-term-rename-input m-0 w-full min-w-0 flex-1 border-0 border-b border-accent bg-transparent p-0 text-[11.5px] font-medium text-inherit outline-none"
                  @blur="commitTabRename"
                  @keydown.enter.prevent="commitTabRename"
                  @keydown.esc.prevent="cancelTabRename"
                  @click.stop
                  @pointerdown.stop
                />
                <span
                  v-else
                  class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-medium"
                  @dblclick.stop="startTabRename(ws, tab)"
                >{{ tab.title }}</span>
                <span
                  v-if="(tab.leafCount ?? 1) > 1"
                  class="inline-flex h-[13px] min-w-[13px] shrink-0 items-center justify-center rounded-md bg-white/[0.08] px-1 text-[9px] font-semibold leading-none text-muted-foreground"
                  :title="`${tab.leafCount} panes`"
                >{{ tab.leafCount }}</span>
                <span
                  v-if="tab.isAgent && (tab.round ?? 0) > 1 && tab.status !== 'idle'"
                  class="min-w-[10px] shrink-0 text-right text-[9px] font-semibold text-muted-foreground opacity-70"
                  :title="`${tab.round} messages sent to agent this session`"
                >↺{{ tab.round }}</span>
                <PhBell
                  v-if="tab.status === 'permission'"
                  :size="11"
                  weight="fill"
                  class="shrink-0 text-[#f59e0b] opacity-90"
                  title="Permission required"
                />
                <span
                  v-if="tab.status && tab.status !== 'idle'"
                  class="status-dot"
                  :class="`status-${tab.status}`"
                  :title="attentionLabel(attentionState(ws.id, tab.id, tab.status))"
                  :aria-label="attentionLabel(attentionState(ws.id, tab.id, tab.status))"
                  role="status"
                >{{ tab.status === 'running' ? spinnerFrame : '' }}</span>
                <PhX
                  :size="9"
                  weight="bold"
                  class="ws-term-close shrink-0 rounded-sm p-px text-muted-foreground opacity-0 transition-opacity group-hover:opacity-50 hover:!opacity-100 hover:!text-destructive"
                  title="Close"
                  data-no-drag
                  @click.stop="termTabs.close(ws.id, tab.id)"
                />
              </div>
            </TransitionGroup>

            <div v-if="!tabCount(ws.id)" class="mx-2 mb-1 ml-[18px] rounded-lg p-2.5 text-center text-[10.5px] leading-[1.7] text-muted-foreground opacity-70">No tabs. Click + to open one.</div>
          </template>
        </div>
      </div>

      <div v-if="store.workspaces.length === 0" class="m-2 rounded-lg border border-dashed border-border/60 p-[30px_20px] text-center text-[11.5px] leading-[1.7] text-muted-foreground">
        No workspaces.<br />Open a folder to start.
      </div>
    </div>

    <div class="shrink-0 border-t border-border px-2 py-1.5">
      <button class="flex w-full items-center justify-center gap-1.5 rounded-md border border-border/65 bg-transparent px-2.5 py-1.5 text-[11px] font-medium text-muted-foreground transition-colors hover:border-border hover:bg-hover hover:text-secondary-foreground active:scale-[0.985]" @click="pickFolder">
        <PhFolderOpen :size="13" />
        Open Folder…
      </button>
    </div>
  </aside>

  <!-- Dialogs teleported to body to escape backdrop-filter stacking context -->
  <Teleport to="body">
    <!-- Rename dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="renameId !== null" @click.self="renameId = null">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-sm font-semibold text-foreground">Rename workspace</h3>
        <input
          v-model="renameName"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="Workspace name"
          @keydown.enter="confirmRename"
          @keydown.esc="renameId = null"
          ref="renameInputEl"
        />
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="renameId = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmRename" :disabled="!renameName.trim()">Rename</button>
        </div>
      </div>
    </div>

    <!-- Name dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="pendingPath" @click.self="pendingPath = ''">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-sm font-semibold text-foreground">Name this workspace</h3>
        <p class="overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-base px-2 py-1.5 font-mono text-[11px] text-secondary-foreground">{{ pendingPath }}</p>
        <input
          v-model="pendingName"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="Workspace name"
          @keydown.enter="confirmCreate"
          @keydown.esc="pendingPath = ''"
          ref="nameInputEl"
        />
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="pendingPath = ''">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmCreate" :disabled="!pendingName.trim()">Create</button>
        </div>
      </div>
    </div>

    <!-- New worktree dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="wtParent" @click.self="closeWtDialog">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-sm font-semibold text-foreground">New worktree — {{ wtParent?.name }}</h3>
        <label class="-mb-1.5 text-[11px] font-semibold text-secondary-foreground">Branch</label>
        <input
          v-model="wtBranch"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="feature/my-branch"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
          ref="wtBranchEl"
        />
        <label class="-mb-1.5 text-[11px] font-semibold text-secondary-foreground">Base branch <span class="font-normal text-muted-foreground">(only for a new branch)</span></label>
        <input
          v-model="wtBase"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="defaults to current HEAD"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
        />
        <datalist id="wt-base-branches">
          <option v-for="b in wtBaseList" :key="b" :value="b" />
        </datalist>
        <p class="overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-base px-2 py-1.5 font-mono text-[11px] text-secondary-foreground">{{ wtTargetPath }}</p>
        <p v-if="wtError" class="whitespace-pre-wrap break-words text-[11px] text-destructive">{{ wtError }}</p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="closeWtDialog">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmWorktree" :disabled="!wtBranch.trim() || wtBusy">
            {{ wtBusy ? "Creating…" : "Create" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted, watch } from "vue";
import {
  PhFolderOpen,
  PhFolder,
  PhCaretDown,
  PhX,
  PhTerminal,
  PhRobot,
  PhBell,
  PhPlus,
  PhKanban,
  PhPushPin,
  PhGitBranch,
  PhChatCenteredText,
} from "@phosphor-icons/vue";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { spinnerFrame } from "@/lib/spinner";
import { usePointerReorder } from "@/composables/usePointerReorder";
import {
  getAgentAttentionState,
  aggregateStatus,
  type AgentAttentionState,
  type TermStatus,
} from "@/lib/terminalStatus";
import { useGitStore, type PrInfo } from "@/stores/git";
import { pinnedIds, isPinned, togglePin } from "@/lib/pinnedWorkspaces";

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();

const active = computed(() => store.active);

function tabCount(id: number): number {
  return (termTabs.tabsByWs[id] || []).length;
}

function repoById(id: number): Workspace | undefined {
  return store.topLevel.find((w) => w.id === id);
}

function rowAggStatus(id: number): TermStatus | null {
  const s = aggregateStatus(termTabs.tabsByWs[id] || [], (t) => t.status);
  return s === "idle" ? null : s;
}

function prTone(info: PrInfo): string {
  if (info.checks === "fail") return "fail";
  if (info.checks === "pending") return "pending";
  if (info.state === "MERGED") return "merged";
  if (info.state === "CLOSED") return "closed";
  if (info.isDraft) return "draft";
  return "open";
}
function prTitle(info: PrInfo): string {
  const state = info.isDraft && info.state === "OPEN" ? "draft" : info.state.toLowerCase();
  const checks = info.checks === "none" ? "" : ` · checks ${info.checks}`;
  return `PR #${info.number} (${state})${checks}`;
}

// ── project filter + grouping ───────────────────────────────────────────────
const filterProjectId = ref<number | null>(null);
const filterOpen = ref(false);
const filterEl = ref<HTMLElement>();
const filterMenuStyle = ref<Record<string, string>>({});

watch(filterOpen, (open) => {
  if (open && filterEl.value) {
    const r = filterEl.value.getBoundingClientRect();
    filterMenuStyle.value = { left: `${r.left + 6}px`, top: `${r.bottom - 4}px`, width: `${r.width - 12}px` };
  }
});

interface ProjectGroup { repo: Workspace; rows: Workspace[] }

// One group per top-level repo (pinned repos first), rows = repo + its worktrees.
const groups = computed<ProjectGroup[]>(() => {
  const repos = filterProjectId.value == null
    ? store.topLevel
    : store.topLevel.filter((r) => r.id === filterProjectId.value);
  const sorted = [...repos].sort((a, b) => {
    const pa = pinnedIds.value.includes(a.id) ? 0 : 1;
    const pb = pinnedIds.value.includes(b.id) ? 0 : 1;
    return pa - pb;
  });
  return sorted.map((repo) => ({
    repo,
    rows: [repo, ...(store.worktreesByParent[repo.id] || [])],
  }));
});

function selectWs(ws: Workspace) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(ws);
  // Keep the repo's worktrees mounted so their status dots stay live.
  const parent = ws.parent_id ?? ws.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
}

// Poll PR status for every workspace + worktree that has a path. gh runs out of
// process and failures cache null, so this never blocks the UI. 60s cadence.
let prTimer: number | undefined;
function refreshAllPrs() {
  // Run through the store's concurrency-capped pool (max 3 gh in flight) so a
  // many-workspace sweep can't spawn N blocking gh subprocesses at once.
  git.fetchPrs(
    store.workspaces
      .filter((ws) => ws.path)
      .map((ws) => ({ wsId: ws.id, cwd: ws.path })),
  );
}

function attentionState(wsId: number, tabId: number, status: TermStatus): AgentAttentionState {
  return getAgentAttentionState(status, termTabs.isCompletionUnseen(wsId, tabId));
}

function attentionLabel(state: AgentAttentionState): string {
  switch (state) {
    case "error": return "Error";
    case "needs-input": return "Needs input";
    case "done-unread": return "Done unread";
    case "working": return "Working";
    default: return "Idle";
  }
}

// ── branch helpers (worktree dialog) ─────────────────────────────────────────
interface GitOutput { stdout: string; stderr: string; code: number; }

async function listBranches(path: string): Promise<string[]> {
  if (git.cwd === path && git.branches.length > 0) return git.branches;
  try {
    const out = await invoke<GitOutput>("run_git", { cwd: path, args: ["branch", "--list"] });
    if (out.code === 0) {
      return out.stdout.split("\n")
        .map(l => l.replace(/^\*?\s+/, "").trim())
        .filter(Boolean);
    }
  } catch {}
  return [];
}

// Mount only the active workspace + its worktree siblings, so each Terminal
// restores its sessions into tabsByWs. NOT every workspace: mounting them all
// had their Terminals race to adopt the daemon's sessions, so a freshly-
// activated workspace showed another one's tabs.
function mountSections() {
  const a = active.value;
  if (!a) return;
  store.ensureOpen(a);
  const parent = a.parent_id ?? a.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
}

function onDocClick() { filterOpen.value = false; }
onMounted(() => {
  document.addEventListener("click", onDocClick);
  mountSections();
  // Defer the first PR sweep off the critical startup path. Firing gh for every
  // workspace synchronously here saturated the Tauri command workers and stalled
  // the real startup invokes (list_workspaces, session restore, create_pty) → the
  // window painted gray for seconds. Let the UI paint first, then poll on the 60s
  // cadence. requestIdleCallback when available; ~2.5s timeout fallback.
  const startPrs = () => { refreshAllPrs(); prTimer = window.setInterval(refreshAllPrs, 60_000); };
  if (typeof window.requestIdleCallback === "function") {
    window.requestIdleCallback(startPrs, { timeout: 2500 });
  } else {
    window.setTimeout(startPrs, 2500);
  }
});
onUnmounted(() => { if (prTimer) clearInterval(prTimer); document.removeEventListener("click", onDocClick); });

// Watch only the STRUCTURE of the workspace set (its id list), not every nested
// property — a deep watch re-ran the mount sweep on any tab/PR mutation.
watch(() => store.workspaces.map(ws => ws.id).join(","), () => { mountSections(); refreshAllPrs(); });
// Switching the active workspace mounts it.
watch(() => active.value?.id, mountSections);

// ── Claude chat sessions ─────────────────────────────────────────────────────
// Rows for every workspace are always listed now (not just opened/pinned
// ones), so a click here may target a workspace with no mounted Terminal yet
// — open it first, or the request has nothing to consume it.
function newChatSession(ws: Workspace) {
  const wasOpen = store.opened.some((w) => w.id === ws.id);
  selectWs(ws);
  const open = () => termTabs.openChat(ws.id);
  wasOpen ? open() : nextTick(open);
}

function newTerminalTab(ws: Workspace) {
  const wasOpen = store.opened.some((w) => w.id === ws.id);
  selectWs(ws);
  const add = () => termTabs.add(ws.id);
  wasOpen ? add() : nextTick(add);
}

// ── drag-to-reorder ──────────────────────────────────────────────────────────
// Pointer-based (not HTML5 DnD): Tauri's native drag-drop handler swallows the
// webview's drop events. Group = workspace id.
const {
  dragIdx: tabDragIdx,
  overIdx: tabOverIdx,
  dragGroup: tabDragGroup,
  down: tabDragDown,
} = usePointerReorder((from, to, group) => {
  if (group != null) termTabs.reorder(Number(group), from, to);
});

// Activate a terminal. Switch to its workspace first if needed (fleet rows can
// point at another one); its Terminal stays mounted while opened.
function selectTab(ws: Workspace, tabId: number) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  if (store.active?.id !== ws.id) store.open(ws);
  nextTick(() => termTabs.activate(ws.id, tabId));
}

// ── tab inline rename ───────────────────────────────────────────────────────
const editingTab = ref<{ wsId: number; tabId: number } | null>(null);
const editingTabTitle = ref("");
let renameReadyAt = 0;

function startTabRename(ws: Workspace, tab: { id: number; title: string }) {
  editingTab.value = { wsId: ws.id, tabId: tab.id };
  editingTabTitle.value = tab.title;
  renameReadyAt = Date.now() + 200; // blur within 200ms of focus = noise, ignore
  nextTick(() => {
    const el = document.querySelector<HTMLInputElement>(".ws-term-rename-input");
    el?.focus();
    el?.select();
  });
}

function commitTabRename() {
  if (Date.now() < renameReadyAt) return; // spurious blur before user could type
  if (!editingTab.value) return;
  const title = editingTabTitle.value.trim();
  if (title) termTabs.rename(editingTab.value.wsId, editingTab.value.tabId, title);
  editingTab.value = null;
}

function cancelTabRename() {
  editingTab.value = null;
}

// ── new worktree dialog ──────────────────────────────────────────────────────
const wtParent = ref<Workspace | null>(null);
const wtBranch = ref("");
const wtBase = ref("");
const wtBaseList = ref<string[]>([]);
const wtBusy = ref(false);
const wtError = ref("");
const wtBranchEl = ref<HTMLInputElement>();

const wtTargetPath = computed(() => {
  if (!wtParent.value) return "";
  const repo = wtParent.value.path.split("/").filter(Boolean).pop() || "repo";
  const branch = wtBranch.value.trim() || "<branch>";
  return `${ui.worktreesDir}/${repo}/${branch}`;
});

async function openWtDialog(parent: Workspace) {
  wtParent.value = parent;
  wtBranch.value = "";
  wtBase.value = "";
  wtError.value = "";
  wtBaseList.value = [];
  await nextTick();
  wtBranchEl.value?.focus();
  wtBaseList.value = await listBranches(parent.path);
}

function closeWtDialog() {
  wtParent.value = null;
}

async function confirmWorktree() {
  const branch = wtBranch.value.trim();
  if (!wtParent.value || !branch || wtBusy.value) return;
  wtBusy.value = true;
  wtError.value = "";
  try {
    const ws = await store.createWorktree(wtParent.value.id, branch, wtBase.value.trim() || null, wtTargetPath.value);
    wtParent.value = null;
    store.open(ws);
  } catch (err) {
    wtError.value = err instanceof Error ? err.message : String(err);
  } finally {
    wtBusy.value = false;
  }
}

// ── rename dialog ──────────────────────────────────────────────────────────
const renameId = ref<number | null>(null);
const renameName = ref("");
const renameInputEl = ref<HTMLInputElement>();

async function startRename(w: Workspace) {
  renameId.value = w.id;
  renameName.value = w.name;
  await nextTick();
  renameInputEl.value?.focus();
  renameInputEl.value?.select();
}
async function confirmRename() {
  const name = renameName.value.trim();
  if (renameId.value === null || !name) return;
  await store.rename(renameId.value, name);
  renameId.value = null;
}

const pendingPath = ref("");
const pendingName = ref("");
const nameInputEl = ref<HTMLInputElement>();

async function pickFolder() {
  const selected = await openDialog({ directory: true, multiple: false });
  if (!selected || typeof selected !== "string") return;
  pendingPath.value = selected;
  pendingName.value = selected.split("/").pop() || selected;
  await nextTick();
  nameInputEl.value?.focus();
  nameInputEl.value?.select();
}

async function confirmCreate() {
  if (!pendingName.value.trim()) return;
  const ws = await store.create(pendingName.value.trim(), pendingPath.value);
  pendingPath.value = "";
  pendingName.value = "";
  store.open(ws);
}
</script>

<style scoped>
.tabs-header.active .tabs-label { color: var(--text-primary); }

.header-unread { animation: pulse-unread 2s ease-in-out infinite; }
@keyframes pulse-unread {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0.55; }
}

.ws-term.active {
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  color: var(--text-primary);
}

/* Preserve the compact list while making each agent state scannable in-place. */
.ws-term.agent-state-idle .ws-term-icon-agent { color: var(--text-muted); }
.ws-term.agent-state-needs-input {
  background: color-mix(in srgb, var(--status-permission) 9%, transparent);
}
.ws-term.agent-state-error {
  background: color-mix(in srgb, var(--red) 10%, transparent);
}
.ws-term.agent-state-done-unread {
  background: color-mix(in srgb, var(--green) 8%, transparent);
}
.ws-term.agent-state-needs-input .ws-term-icon-agent { color: var(--status-permission); }
.ws-term.agent-state-error .ws-term-icon-agent { color: var(--red); }
.ws-term.agent-state-done-unread .ws-term-icon-agent { color: var(--green); }

/* ── FLIP reorder animation ────────────────────────────────────── */
.ws-move-move { transition: transform .22s cubic-bezier(.2, .8, .2, 1); }

@keyframes pr-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.55; } }
</style>
