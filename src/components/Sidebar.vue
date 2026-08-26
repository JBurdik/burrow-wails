<template>
  <aside class="flex w-[var(--sidebar-width,220px)] shrink-0 grow-0 basis-[var(--sidebar-width,220px)] flex-col overflow-hidden border-r border-border bg-panel [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)]">
    <!-- Workspace selector (repos + their worktrees) -->
    <WorkspacePicker
      @pick-folder="pickFolder"
      @rename="startRename"
      @new-worktree="openWtDialog"
    />

    <!-- Blocking attention: errors and input requests. -->
    <div v-if="blockingItems.length > 0" class="mx-1.5 mb-1.5 shrink-0 overflow-hidden rounded-[7px] border border-[color-mix(in_srgb,var(--red)_35%,var(--border))] bg-base">
      <div class="flex items-center gap-1.5 border-b border-border px-2 pb-1 pt-1.5 text-[9px] font-semibold uppercase tracking-[0.07em] text-muted-foreground">
        <PhWarningCircle :size="11" class="shrink-0 text-destructive" weight="fill" />
        <span>Needs Attention</span>
        <span class="ml-auto rounded-lg bg-destructive/[0.15] px-1.5 py-px text-[9px] font-bold leading-[1.6] text-destructive">{{ blockingItems.length }}</span>
      </div>
      <button
        v-for="item in blockingItems"
        :key="`att-${item.wsId}-${item.tabId}`"
        type="button"
        class="group flex w-full items-center gap-[7px] border-0 border-b border-border/40 bg-transparent px-2 py-[5px] text-left font-inherit text-inherit transition-colors last:border-b-0 hover:bg-hover focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:-outline-offset-2"
        :class="item.attention === 'needs-input' && 'bg-[color-mix(in_srgb,var(--status-permission)_6%,transparent)]'"
        :aria-label="`${item.tabTitle}, ${attentionLabel(item.attention)}, ${item.wsName}`"
        @click="selectFleetItem(item)"
      >
        <span class="status-dot w-3.5 shrink-0 text-center" :class="`status-${item.status}`" aria-hidden="true">{{ item.status === 'running' ? spinnerFrame : '' }}</span>
        <div class="flex min-w-0 flex-1 flex-col gap-px">
          <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-medium text-foreground">{{ item.tabTitle }}</span>
          <span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[9.5px] text-muted-foreground">{{ attentionLabel(item.attention) }} · {{ item.wsName }}</span>
        </div>
        <PhArrowRight :size="9" class="shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-60" />
      </button>
    </div>

    <!-- Finished while away: distinct from blockers, persists until opened. -->
    <div v-if="completionItems.length > 0" class="mx-1.5 mb-1.5 shrink-0 overflow-hidden rounded-[7px] border border-[color-mix(in_srgb,var(--green)_32%,var(--border))] bg-base">
      <div class="flex items-center gap-1.5 border-b border-border px-2 pb-1 pt-1.5 text-[9px] font-semibold uppercase tracking-[0.07em] text-muted-foreground">
        <PhCheckCircle :size="11" class="shrink-0 text-success" weight="fill" />
        <span>Done unread</span>
        <span class="ml-auto rounded-lg bg-success/[0.15] px-1.5 py-px text-[9px] font-bold leading-[1.6] text-success">{{ completionItems.length }}</span>
      </div>
      <button
        v-for="item in completionItems"
        :key="`done-${item.wsId}-${item.tabId}`"
        type="button"
        class="group flex w-full items-center gap-[7px] border-0 border-b border-border/40 bg-transparent px-2 py-[5px] text-left font-inherit text-inherit transition-colors last:border-b-0 hover:bg-hover focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:-outline-offset-2"
        :aria-label="`${item.tabTitle}, done unread, ${item.wsName}`"
        @click="selectFleetItem(item)"
      >
        <span class="status-dot w-3.5 shrink-0 text-center" :class="`status-${item.status}`" aria-hidden="true" />
        <div class="flex min-w-0 flex-1 flex-col gap-px">
          <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-medium text-foreground">{{ item.tabTitle }}</span>
          <span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[9.5px] text-muted-foreground">Done unread · {{ item.wsName }}</span>
        </div>
        <PhArrowRight :size="9" class="shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-60" />
      </button>
    </div>

    <!-- Fleet strip: all non-idle agents across all workspaces -->
    <div v-if="fleetItems.length > 0" class="mx-1.5 mb-1.5 shrink-0 overflow-hidden rounded-[7px] border border-border bg-base">
      <div class="flex items-center gap-1.5 border-b border-border px-2 pb-1 pt-1.5 text-[9px] font-semibold uppercase tracking-[0.07em] text-muted-foreground">
        <PhActivity :size="10" class="shrink-0 text-accent" />
        <span>Agents</span>
        <span class="ml-auto rounded-lg bg-accent/[0.15] px-1.5 py-px text-[9px] font-bold leading-[1.6] text-accent">{{ fleetItems.length }}</span>
      </div>
      <button
        v-for="item in fleetItems"
        :key="`${item.wsId}-${item.tabId}`"
        type="button"
        class="group flex w-full items-center gap-[7px] border-0 border-b border-border/40 bg-transparent px-2 py-[5px] text-left font-inherit text-inherit transition-colors last:border-b-0 hover:bg-hover focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:-outline-offset-2"
        :aria-label="`${item.tabTitle}, working, ${item.wsName}`"
        @click="selectFleetItem(item)"
      >
        <span class="status-dot w-3.5 shrink-0 text-center" :class="`status-${item.status}`" aria-hidden="true">{{ item.status === 'running' ? spinnerFrame : '' }}</span>
        <div class="flex min-w-0 flex-1 flex-col gap-px">
          <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] font-medium text-foreground">{{ item.tabTitle }}</span>
          <span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[9.5px] text-muted-foreground">{{ item.wsName }}</span>
        </div>
        <PhArrowRight :size="9" class="shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-60" />
      </button>
    </div>

    <!-- One section per visible workspace: the pinned ones plus the active one -->
    <div class="flex-1 overflow-y-auto py-0.5 pb-1.5">
      <div v-for="ws in sections" :key="ws.id" class="mb-1">
        <div class="tabs-header group/header mx-1 flex cursor-pointer items-center gap-[5px] rounded-md py-[5px] pb-1 pl-2.5 pr-2 transition-colors hover:bg-hover" :class="active?.id === ws.id && 'active'" @click="selectWs(ws)">
          <PhPushPin v-if="isPinned(ws.id)" :size="10" weight="fill" class="shrink-0 text-accent" />
          <PhGitBranch v-else-if="ws.parent_id" :size="11" class="shrink-0 text-[#a78bfa]" />
          <span class="tabs-label overflow-hidden text-ellipsis whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.07em] text-muted-foreground">{{ ws.worktree_branch || ws.name }}</span>
          <span v-if="tabCount(ws.id)" class="rounded-lg bg-hover px-1.5 py-px text-[9px] font-semibold leading-[1.6] text-muted-foreground">{{ tabCount(ws.id) }}</span>
          <span v-if="sections.length === 1 && unreadCount > 0" class="header-unread rounded-lg bg-success px-1.5 py-px text-[9px] font-bold leading-[1.6] text-white" :title="`${unreadCount} unread — ⌘⇧U to jump`">{{ unreadCount }}</span>
          <div class="ml-auto flex items-center gap-px opacity-0 transition-opacity group-hover/header:opacity-100" :class="active?.id === ws.id && '!opacity-100'">
            <button class="flex items-center rounded-md p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="Board" @click.stop="ui.openBoard(ws.id)"><PhKanban :size="13" /></button>
            <button class="flex items-center rounded-md p-1 text-[#d97706] transition-colors hover:bg-[#d97706]/[0.14] active:scale-90" title="New conversation" aria-label="New conversation" @click.stop="newChatSession(ws.id)"><PhChatCenteredText :size="13" /></button>
            <button class="flex items-center rounded-md p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="New terminal" @click.stop="termTabs.add(ws.id)"><PhPlus :size="13" /></button>
          </div>
        </div>

        <TransitionGroup name="ws-move" tag="div" class="ws-terminals ml-3.5 mb-[3px] mr-1.5 mt-px flex flex-col gap-px border-l border-border/55 pl-1.5">
          <div
            v-for="(tab, tabIdx) in termTabs.tabsByWs[ws.id] || []"
            :key="tab.id"
            class="ws-term group relative flex touch-none items-center gap-1.5 rounded-md px-[7px] py-[5px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
            :data-reorder-idx="tabIdx"
            :data-reorder-group="String(ws.id)"
            :class="{
              active: active?.id === ws.id && termTabs.activeByWs[ws.id] === tab.id,
              [`agent-state-${attentionState(ws.id, tab.id, tab.status)}`]: tab.isAgent,
              'drag-over outline outline-1 outline-accent -outline-offset-1 bg-hover': tabDragGroup === String(ws.id) && tabOverIdx === tabIdx && tabDragIdx !== tabIdx,
              'dragging opacity-40': tabDragGroup === String(ws.id) && tabDragIdx === tabIdx,
            }"
            @click.stop="selectTab(ws, tab.id)"
            @dblclick.stop
            @pointerdown="(e: PointerEvent) => tabDragDown(tabIdx, e, String(ws.id))"
          >
            <PhChatCenteredText v-if="tab.isChat" :size="11" class="claude-session-icon shrink-0 text-[#d97706]" :class="active?.id === ws.id && termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
            <PhRobot v-else-if="tab.isAgent" :size="11" class="ws-term-icon-agent shrink-0 text-accent" :class="active?.id === ws.id && termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
            <PhTerminal v-else :size="11" class="shrink-0 text-muted-foreground" :class="active?.id === ws.id && termTabs.activeByWs[ws.id] === tab.id && '!text-accent'" />
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
  PhX,
  PhTerminal,
  PhRobot,
  PhBell,
  PhPlus,
  PhKanban,
  PhPushPin,
  PhGitBranch,
  PhActivity,
  PhArrowRight,
  PhWarningCircle,
  PhCheckCircle,
  PhChatCenteredText,
} from "@phosphor-icons/vue";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import WorkspacePicker from "@/components/WorkspacePicker.vue";
import { spinnerFrame } from "@/lib/spinner";
import { usePointerReorder } from "@/composables/usePointerReorder";
import {
  ATTENTION_PRIORITY,
  getAgentAttentionState,
  type AgentAttentionState,
  type TermStatus,
} from "@/lib/terminalStatus";
import { useGitStore } from "@/stores/git";
import { pinnedIds, isPinned } from "@/lib/pinnedWorkspaces";

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();

const active = computed(() => store.active);

function tabCount(id: number): number {
  return (termTabs.tabsByWs[id] || []).length;
}

// Workspaces that get their own tab section: every pinned one, in pin order,
// plus the active one when it isn't pinned (so selecting from the dropdown
// always shows its tabs without forcing a pin).
const sections = computed<Workspace[]>(() => {
  const byId = (id: number) => store.workspaces.find((w) => w.id === id);
  const pinned = pinnedIds.value.map(byId).filter((w): w is Workspace => !!w);
  const a = active.value;
  return a && !pinned.some((w) => w.id === a.id) ? [...pinned, a] : pinned;
});

function selectWs(ws: Workspace) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(ws);
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

// Count of completed agent turns not yet acknowledged. Drives the unread badge.
const unreadCount = computed(() => {
  let n = 0;
  for (const [wsId, tabs] of Object.entries(termTabs.tabsByWs)) {
    n += tabs.filter((tab) => attentionState(Number(wsId), tab.id, tab.status) === "done-unread").length;
  }
  return n;
});

// ── fleet view ────────────────────────────────────────────────────────────────
interface FleetItem {
  wsId: number;
  wsName: string;
  tabId: number;
  tabTitle: string;
  status: TermStatus;
  attention: AgentAttentionState;
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

const fleetItems = computed<FleetItem[]>(() => {
  const items: FleetItem[] = [];
  for (const ws of store.workspaces) {
    for (const tab of termTabs.tabsByWs[ws.id] ?? []) {
      const attention = attentionState(ws.id, tab.id, tab.status);
      if (attention === "working") {
        items.push({ wsId: ws.id, wsName: ws.name, tabId: tab.id, tabTitle: tab.title, status: tab.status, attention });
      }
    }
  }
  return items;
});

function selectFleetItem(item: FleetItem) {
  const ws = store.workspaces.find((w) => w.id === item.wsId);
  if (ws) selectTab(ws, item.tabId);
}

// ── needs-attention strip ───────────────────────────────────────────────────
// Tabs across every workspace/worktree that require action or acknowledgement.
const attentionItems = computed<FleetItem[]>(() => {
  const items: FleetItem[] = [];
  for (const ws of store.workspaces) {
    for (const tab of termTabs.tabsByWs[ws.id] ?? []) {
      const attention = attentionState(ws.id, tab.id, tab.status);
      if (attention === "error" || attention === "needs-input" || attention === "done-unread") {
        items.push({ wsId: ws.id, wsName: ws.name, tabId: tab.id, tabTitle: tab.title, status: tab.status, attention });
      }
    }
  }
  return items.sort(
    (a, b) => ATTENTION_PRIORITY.indexOf(a.attention) - ATTENTION_PRIORITY.indexOf(b.attention),
  );
});

const blockingItems = computed(() => attentionItems.value.filter((item) => item.attention !== "done-unread"));
const completionItems = computed(() => attentionItems.value.filter((item) => item.attention === "done-unread"));

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

// Mount the workspaces that have a visible tab section (active + pinned) plus
// their worktrees, so each Terminal restores its sessions into tabsByWs.
// NOT every workspace: mounting them all had their Terminals race to adopt the
// daemon's sessions, so a freshly-activated workspace showed another one's tabs.
function mountSections() {
  for (const ws of sections.value) {
    store.ensureOpen(ws);
    const parent = ws.parent_id ?? ws.id;
    for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
  }
}

onMounted(() => {
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
onUnmounted(() => { if (prTimer) clearInterval(prTimer); });

// Watch only the STRUCTURE of the workspace set (its id list), not every nested
// property — a deep watch re-ran the mount sweep on any tab/PR mutation.
watch(() => store.workspaces.map(ws => ws.id).join(","), () => { mountSections(); refreshAllPrs(); });
// Pinning a workspace (or switching to one) gives it a section — mount it.
watch(() => sections.value.map(ws => ws.id).join(","), mountSections);

// ── Claude chat sessions ─────────────────────────────────────────────────────
function newChatSession(workspaceId: number) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  termTabs.openChat(workspaceId);
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
</style>
