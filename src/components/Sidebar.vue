<template>
  <aside class="sidebar">
    <div class="sidebar-header">
      <div class="header-title">
        <span class="header-label">Workspaces</span>
        <span v-if="store.topLevel.length" class="header-count">{{ store.topLevel.length }}</span>
        <span
          v-if="unreadCount > 0"
          class="header-unread"
          :title="`${unreadCount} unread — ⌘⇧U to jump`"
        >{{ unreadCount }}</span>
      </div>
      <button class="icon-btn" title="Open folder" @click="pickFolder">
        <PhFolderPlus :size="15" />
      </button>
    </div>

    <!-- Needs Attention: tabs that need the user to look (error/permission/waiting/review) -->
    <div v-if="attentionItems.length > 0" class="attention-strip">
      <div class="attention-header">
        <PhWarningCircle :size="11" class="attention-header-icon" weight="fill" />
        <span>Needs Attention</span>
        <span class="attention-count">{{ attentionItems.length }}</span>
      </div>
      <div
        v-for="item in attentionItems"
        :key="`att-${item.wsId}-${item.tabId}`"
        class="attention-row"
        :class="`attention-${item.status}`"
        @click="selectFleetItem(item)"
      >
        <span class="attention-dot status-dot" :class="`status-${item.status}`">{{ item.status === 'running' ? spinnerFrame : '' }}</span>
        <div class="attention-info">
          <span class="attention-tab">{{ item.tabTitle }}</span>
          <span class="attention-ws">{{ item.wsName }}</span>
        </div>
        <PhArrowRight :size="9" class="attention-arrow" />
      </div>
    </div>

    <!-- Fleet strip: all non-idle agents across all workspaces -->
    <div v-if="fleetItems.length > 0" class="fleet-strip">
      <div class="fleet-header">
        <PhActivity :size="10" class="fleet-header-icon" />
        <span>Agents</span>
        <span class="fleet-count">{{ fleetItems.length }}</span>
      </div>
      <div
        v-for="item in fleetItems"
        :key="`${item.wsId}-${item.tabId}`"
        class="fleet-row"
        :class="`fleet-${item.status}`"
        @click="selectFleetItem(item)"
      >
        <span class="fleet-dot status-dot" :class="`status-${item.status}`">{{ item.status === 'running' ? spinnerFrame : '' }}</span>
        <div class="fleet-info">
          <span class="fleet-tab">{{ item.tabTitle }}</span>
          <span class="fleet-ws">{{ item.wsName }}</span>
        </div>
        <PhArrowRight :size="9" class="fleet-arrow" />
      </div>
    </div>

    <div class="ws-list">
      <TransitionGroup name="ws-move" tag="div" class="ws-list-inner">
      <div
        v-for="(item, wsIdx) in store.topLevel"
        :key="item.id"
        class="ws-group"
        :data-reorder-idx="wsIdx"
        data-reorder-group="ws"
      >
        <div
          class="ws-item"
          :class="{
            active: store.active?.id === item.id,
            'drag-over': wsOverIdx === wsIdx && wsDragIdx !== wsIdx,
            dragging: wsDragIdx === wsIdx,
          }"
          @click="openWs(item)"
          @pointerdown="(e: PointerEvent) => wsDragDown(wsIdx, e, 'ws')"
          @contextmenu.prevent.stop="openCtxMenu(item, $event)"
        >
          <button class="ws-caret" :title="isCollapsed(item.id) ? 'Expand' : 'Collapse'" data-no-drag @click.stop="toggleCollapse(item.id)">
            <PhCaretRight v-if="isCollapsed(item.id)" :size="14" weight="bold" />
            <PhCaretDown v-else :size="14" weight="bold" />
          </button>
          <div class="ws-icon-wrap" title="Change icon via right-click menu">
            <img v-if="store.icons[item.id]" :src="store.icons[item.id]" class="ws-custom-icon" />
            <PhFolder v-else :size="14" weight="fill" class="ws-icon" />
          </div>
          <div class="ws-info">
            <div class="ws-name">{{ item.name }}</div>
            <div class="ws-path">{{ shortPath(item.path) }}</div>
          </div>
          <span
            v-if="git.prByWs[item.id]"
            class="pr-badge"
            :class="`pr-${prTone(git.prByWs[item.id]!)}`"
            :title="prTitle(git.prByWs[item.id]!)"
          >
            <span class="pr-dot" />#{{ git.prByWs[item.id]!.number }}
          </span>
          <button class="ws-add-chat" title="Board" data-no-drag @click.stop="ui.openBoard(item.id)">
            <PhKanban :size="12" />
          </button>
          <button class="ws-add-chat" title="New chat" data-no-drag @click.stop="newChatSession(item.id)">
            <ClaudeIcon :size="12" />
            <PhPlus :size="8" weight="bold" class="ws-add-chat-plus" />
          </button>
          <button class="ws-delete" title="Remove" data-no-drag @click.stop="store.remove(item.id)">
            <PhX :size="10" />
          </button>
        </div>

        <!-- Terminal tabs -->
        <TransitionGroup
          v-if="!isCollapsed(item.id) && termTabs.tabsByWs[item.id]?.length"
          name="ws-move"
          tag="div"
          class="ws-terminals"
        >
          <div
            v-for="(tab, tabIdx) in termTabs.tabsByWs[item.id]"
            :key="tab.id"
            class="ws-term"
            :data-reorder-idx="tabIdx"
            :data-reorder-group="String(item.id)"
            :class="{
              active:
                store.active?.id === item.id && termTabs.activeByWs[item.id] === tab.id,
              'drag-over':
                tabDragGroup === String(item.id) && tabOverIdx === tabIdx && tabDragIdx !== tabIdx,
              dragging: tabDragGroup === String(item.id) && tabDragIdx === tabIdx,
            }"
            @click.stop="selectTab(item, tab.id)"
            @dblclick.stop
            @pointerdown="(e: PointerEvent) => tabDragDown(tabIdx, e, String(item.id))"
          >
            <ClaudeIcon v-if="tab.isChat" :size="11" class="ws-term-icon claude-session-icon" />
            <PhRobot v-else-if="tab.isAgent" :size="11" class="ws-term-icon agent" />
            <PhTerminal v-else :size="11" class="ws-term-icon" />
            <input
              v-if="editingTab?.wsId === item.id && editingTab?.tabId === tab.id"
              v-model="editingTabTitle"
              class="ws-term-rename-input"
              @blur="commitTabRename"
              @keydown.enter.prevent="commitTabRename"
              @keydown.esc.prevent="cancelTabRename"
              @click.stop
              @pointerdown.stop
            />
            <span
              v-else
              class="ws-term-label"
              @dblclick.stop="startTabRename(item, tab)"
            >{{ tab.title }}</span>
            <span
              v-if="(tab.leafCount ?? 1) > 1"
              class="ws-term-split-count"
              :title="`${tab.leafCount} panes`"
            >{{ tab.leafCount }}</span>
            <span
              v-if="tab.isAgent && (tab.round ?? 0) > 1 && tab.status !== 'idle'"
              class="ws-term-round"
              :title="`${tab.round} messages sent to agent this session`"
            >↺{{ tab.round }}</span>
            <PhBell
              v-if="tab.status === 'permission'"
              :size="11"
              weight="fill"
              class="ws-term-permission-bell"
              title="Permission required"
            />
            <span
              v-if="tab.status && tab.status !== 'idle'"
              class="status-dot"
              :class="`status-${tab.status}`"
            >{{ tab.status === 'running' ? spinnerFrame : '' }}</span>
            <PhX
              :size="9"
              weight="bold"
              class="ws-term-close"
              title="Close"
              data-no-drag
              @click.stop="termTabs.close(item.id, tab.id)"
            />
          </div>
        </TransitionGroup>

        <!-- Worktrees subsection — only when worktrees exist -->
        <div v-if="!isCollapsed(item.id) && (store.worktreesByParent[item.id]?.length ?? 0) > 0" class="ws-worktrees">
          <div class="ws-worktree-head">
            <span>Worktrees</span>
            <button class="icon-btn" title="New worktree" @click.stop="openWtDialog(item)">
              <PhPlus :size="11" />
            </button>
          </div>
          <template v-for="wt in store.worktreesByParent[item.id] || []" :key="wt.id">
            <div
              class="ws-term ws-worktree"
              :class="{ active: store.active?.id === wt.id }"
              :title="wt.path"
              @click.stop="selectWorktree(wt)"
              @contextmenu.prevent.stop="openWtCtxMenu(wt, $event)"
            >
              <PhGitBranch :size="11" class="ws-term-icon" />
              <span class="ws-term-label">{{ wt.worktree_branch || wt.name }}</span>
              <span
                v-if="git.prByWs[wt.id]"
                class="pr-badge"
                :class="`pr-${prTone(git.prByWs[wt.id]!)}`"
                :title="prTitle(git.prByWs[wt.id]!)"
              >
                <span class="pr-dot" />#{{ git.prByWs[wt.id]!.number }}
              </span>
              <span
                v-if="aggStatus(wt.id)"
                class="status-dot"
                :class="`status-${aggStatus(wt.id)}`"
              >{{ aggStatus(wt.id) === 'running' ? spinnerFrame : '' }}</span>
              <button class="ws-add-chat" title="New chat" data-no-drag @click.stop="newChatSession(wt.id)">
                <ClaudeIcon :size="12" />
                <PhPlus :size="8" weight="bold" class="ws-add-chat-plus" />
              </button>
            </div>

            <!-- worktree's own terminal tabs -->
            <div v-if="termTabs.tabsByWs[wt.id]?.length" class="ws-terminals ws-wt-terminals">
              <div
                v-for="tab in termTabs.tabsByWs[wt.id]"
                :key="tab.id"
                class="ws-term"
                :class="{
                  active:
                    store.active?.id === wt.id && termTabs.activeByWs[wt.id] === tab.id,
                }"
                @click.stop="selectTab(wt, tab.id)"
              >
                <PhRobot v-if="tab.isAgent" :size="11" class="ws-term-icon agent" />
                <PhTerminal v-else :size="11" class="ws-term-icon" />
                <span class="ws-term-label">{{ tab.title }}</span>
                <span
                  v-if="(tab.leafCount ?? 1) > 1"
                  class="ws-term-split-count"
                  :title="`${tab.leafCount} panes`"
                >{{ tab.leafCount }}</span>
                <span
                  v-if="tab.status && tab.status !== 'idle'"
                  class="status-dot"
                  :class="`status-${tab.status}`"
                >{{ tab.status === 'running' ? spinnerFrame : '' }}</span>
                <PhX
                  :size="9"
                  weight="bold"
                  class="ws-term-close"
                  title="Close"
                  data-no-drag
                  @click.stop="termTabs.close(wt.id, tab.id)"
                />
              </div>
            </div>
          </template>
        </div>
      </div>
      </TransitionGroup>

      <div v-if="store.workspaces.length === 0" class="ws-empty">
        No workspaces.<br />Click + to open a folder.
      </div>
    </div>

    <div class="sidebar-footer">
      <button class="footer-btn" @click="pickFolder">
        <PhFolderOpen :size="13" />
        Open Folder…
      </button>
    </div>

    <!-- Workspace context menu -->
    <Teleport to="body">
      <div
        v-if="ctxMenu"
        class="ctx-menu"
        :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }"
        @click.stop
        @contextmenu.prevent.stop
      >
        <button class="ctx-item" @click="ctxOpen()"><PhFolderOpen :size="13" />Open</button>
        <button class="ctx-item" @click="ctxRename()"><PhPencilSimple :size="13" />Rename…</button>
        <button class="ctx-item" @click="ctxIcon()"><PhImage :size="13" />Change icon…</button>
        <button v-if="store.icons[ctxMenu.wsId]" class="ctx-item" @click="ctxClearIcon()"><PhImage :size="13" />Reset icon</button>
        <div class="ctx-sep" />
        <button class="ctx-item ctx-danger" @click="ctxRemove()"><PhTrash :size="13" />Remove</button>
      </div>
    </Teleport>

    <!-- Worktree context menu -->
    <Teleport to="body">
      <div
        v-if="wtCtxMenu"
        class="ctx-menu"
        :style="{ left: wtCtxMenu.x + 'px', top: wtCtxMenu.y + 'px' }"
        @click.stop
        @contextmenu.prevent.stop
      >
        <button class="ctx-item" @click="wtCtxOpen()"><PhFolderOpen :size="13" />Open</button>
        <div class="ctx-sep" />
        <button class="ctx-item ctx-danger" @click="wtCtxRemove()"><PhTrash :size="13" />Remove worktree</button>
      </div>
    </Teleport>

  </aside>

  <!-- Dialogs teleported to body to escape backdrop-filter stacking context -->
  <Teleport to="body">
    <!-- Rename dialog -->
    <div class="dialog-overlay" v-if="renameId !== null" @click.self="renameId = null">
      <div class="dialog">
        <h3>Rename workspace</h3>
        <input
          v-model="renameName"
          class="dialog-input"
          placeholder="Workspace name"
          @keydown.enter="confirmRename"
          @keydown.esc="renameId = null"
          ref="renameInputEl"
        />
        <div class="dialog-actions">
          <button class="btn-secondary" @click="renameId = null">Cancel</button>
          <button class="btn-primary" @click="confirmRename" :disabled="!renameName.trim()">Rename</button>
        </div>
      </div>
    </div>

    <!-- Name dialog -->
    <div class="dialog-overlay" v-if="pendingPath" @click.self="pendingPath = ''">
      <div class="dialog">
        <h3>Name this workspace</h3>
        <p class="dialog-path">{{ pendingPath }}</p>
        <input
          v-model="pendingName"
          class="dialog-input"
          placeholder="Workspace name"
          @keydown.enter="confirmCreate"
          @keydown.esc="pendingPath = ''"
          ref="nameInputEl"
        />
        <div class="dialog-actions">
          <button class="btn-secondary" @click="pendingPath = ''">Cancel</button>
          <button class="btn-primary" @click="confirmCreate" :disabled="!pendingName.trim()">Create</button>
        </div>
      </div>
    </div>

    <!-- New worktree dialog -->
    <div class="dialog-overlay" v-if="wtParent" @click.self="closeWtDialog">
      <div class="dialog">
        <h3>New worktree — {{ wtParent?.name }}</h3>
        <label class="wt-label">Branch</label>
        <input
          v-model="wtBranch"
          class="dialog-input"
          placeholder="feature/my-branch"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
          ref="wtBranchEl"
        />
        <label class="wt-label">Base branch <span class="wt-hint">(only for a new branch)</span></label>
        <input
          v-model="wtBase"
          class="dialog-input"
          placeholder="defaults to current HEAD"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
        />
        <datalist id="wt-base-branches">
          <option v-for="b in wtBaseList" :key="b" :value="b" />
        </datalist>
        <p class="dialog-path">{{ wtTargetPath }}</p>
        <p v-if="wtError" class="wt-error">{{ wtError }}</p>
        <div class="dialog-actions">
          <button class="btn-secondary" @click="closeWtDialog">Cancel</button>
          <button class="btn-primary" @click="confirmWorktree" :disabled="!wtBranch.trim() || wtBusy">
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
  PhFolderPlus,
  PhFolder,
  PhFolderOpen,
  PhX,
  PhTerminal,
  PhRobot,
  PhBell,
  PhPlus,
  PhKanban,
  PhGitBranch,
  PhPencilSimple,
  PhImage,
  PhTrash,
  PhCaretRight,
  PhCaretDown,
  PhActivity,
  PhArrowRight,
  PhWarningCircle,
} from "@phosphor-icons/vue";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import { spinnerFrame } from "@/lib/spinner";
import { usePointerReorder } from "@/composables/usePointerReorder";
import { aggregateStatus, STATUS_PRIORITY, type TermStatus } from "@/lib/terminalStatus";
import { useGitStore, type PrInfo } from "@/stores/git";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();

// ── collapse / expand per workspace ──────────────────────────────────────────
const COLLAPSE_KEY = "burrow.ws.collapsed";
const CONFIG_COLLAPSE_KEY = "sidebarCollapseState";
const collapsed = ref<Record<number, boolean>>({});

configReady.then(() => {
  migrateFromLocalStorage(COLLAPSE_KEY, CONFIG_COLLAPSE_KEY);
  collapsed.value = getConfig<Record<number, boolean>>(CONFIG_COLLAPSE_KEY, {});
});

// Default collapsed: a workspace with no stored value reads as collapsed.
function isCollapsed(id: number): boolean {
  return collapsed.value[id] !== false;
}
function setCollapsed(id: number, val: boolean) {
  collapsed.value[id] = val;
  setConfig(CONFIG_COLLAPSE_KEY, collapsed.value);
}
function toggleCollapse(id: number) {
  const next = !isCollapsed(id);
  setCollapsed(id, next);
  // Expanding a never-opened workspace shows no tabs until its Terminal mounts.
  // Worktrees need the same eager mount, else their nested rows list no terminals.
  if (!next) { const w = store.workspaces.find((x) => x.id === id); if (w) store.open(w); mountWorktrees(id); }
}

// Item click: activate + expand the workspace. Never collapses — collapse is the
// caret's job only (clicking the row to select shouldn't fold the tab list shut).
function openWs(item: Workspace) {
  if (isCollapsed(item.id)) setCollapsed(item.id, false);
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(item);
  mountWorktrees(item.id);
}

// Worktree row click: just open/activate it (its terminals are already mounted
// via mountWorktrees; no per-worktree collapse state to toggle).
function selectWorktree(wt: Workspace) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(wt);
}

// Mount every worktree of an expanded parent so each one's Terminal restores its
// saved/daemon sessions into tabsByWs — otherwise the nested rows show no tabs.
function mountWorktrees(parentId: number) {
  for (const wt of store.worktreesByParent[parentId] || []) store.ensureOpen(wt);
}

// Aggregate status of a workspace's tabs (highest-priority wins). Drives the
// worktree row dot so a finished/working agent is visible without expanding.
// Returns null for idle (no dot to show).
function aggStatus(id: number): TermStatus | null {
  const tabs = termTabs.tabsByWs[id] || [];
  const s = aggregateStatus(tabs, (t) => t.status);
  return s === "idle" ? null : s;
}

// ── PR status badge ──────────────────────────────────────────────────────────
// Visual tone for a PR badge: failing CI wins, then state, then draft.
function prTone(info: PrInfo): string {
  if (info.checks === "fail") return "fail";
  if (info.checks === "pending") return "pending"; // CI in flight — amber, never green
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

// Count of tabs with "review" status across ALL workspaces (agent finished while
// user wasn't watching). Drives the unread badge in the sidebar header.
const unreadCount = computed(() => {
  let n = 0;
  for (const tabs of Object.values(termTabs.tabsByWs)) {
    n += tabs.filter((t) => t.status === "review").length;
  }
  return n;
});

// ── branch helpers (listBranches used by worktree dialog) ────────────────────
// ── fleet view ────────────────────────────────────────────────────────────────
interface FleetItem { wsId: number; wsName: string; tabId: number; tabTitle: string; status: TermStatus; }

const fleetItems = computed<FleetItem[]>(() => {
  const items: FleetItem[] = [];
  for (const ws of store.workspaces) {
    for (const tab of termTabs.tabsByWs[ws.id] ?? []) {
      if (tab.status !== "idle") {
        items.push({ wsId: ws.id, wsName: ws.name, tabId: tab.id, tabTitle: tab.title, status: tab.status });
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
// Tabs (across every workspace + worktree) whose status means the user should
// look: error / permission / waiting / review. Pinned at the top, sorted by
// STATUS_PRIORITY (most urgent first). Reactive to status changes via tabsByWs.
const ATTENTION_STATES = new Set<TermStatus>(["error", "permission", "waiting", "review"]);

const attentionItems = computed<FleetItem[]>(() => {
  const items: FleetItem[] = [];
  for (const ws of store.workspaces) {
    for (const tab of termTabs.tabsByWs[ws.id] ?? []) {
      if (ATTENTION_STATES.has(tab.status)) {
        items.push({ wsId: ws.id, wsName: ws.name, tabId: tab.id, tabTitle: tab.title, status: tab.status });
      }
    }
  }
  return items.sort(
    (a, b) => STATUS_PRIORITY.indexOf(a.status) - STATUS_PRIORITY.indexOf(b.status),
  );
});

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

onMounted(() => {
  store.workspaces.forEach(ws => {
    if (!isCollapsed(ws.id)) { store.open(ws); mountWorktrees(ws.id); }
  });
  document.addEventListener("click", () => { ctxMenu.value = null; wtCtxMenu.value = null; });
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
// Re-poll when the workspace list changes (new repo/worktree added).
watch(() => store.workspaces.length, refreshAllPrs);
// Watch only the STRUCTURE of the workspace set (its id list), not every nested
// property. A deep watch here re-ran ensureOpen + mountWorktrees over ALL
// workspaces on any mutation (e.g. a PR-status or tab change), piling git
// subprocesses on a workspace switch. Keyed on the joined ids, it fires only
// when workspaces are actually added/removed — same behavior, far fewer runs.
watch(() => store.workspaces.map(ws => ws.id).join(","), () => store.workspaces.forEach(ws => {
  if (!ws.parent_id && !isCollapsed(ws.id)) { store.ensureOpen(ws); mountWorktrees(ws.id); }
}));

// ── Claude chat sessions ─────────────────────────────────────────────────────
// Hide the per-repo Manager (control) session from the list — it lives in the
// floating Manager card, not as a regular chat tab.
function newChatSession(workspaceId: number) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  if (store.active?.id !== workspaceId) {
    const w = store.workspaces.find((x) => x.id === workspaceId);
    if (w) store.open(w);
  }
  termTabs.openChat(workspaceId);
}

// ── drag-to-reorder ──────────────────────────────────────────────────────────
// Pointer-based (not HTML5 DnD): Tauri's native drag-drop handler swallows the
// webview's drop events. Group = workspace id, so a tab only reorders within its
// own project's list.
const {
  dragIdx: tabDragIdx,
  overIdx: tabOverIdx,
  dragGroup: tabDragGroup,
  down: tabDragDown,
} = usePointerReorder((from, to, group) => {
  if (group != null) termTabs.reorder(Number(group), from, to);
});

// Top-level workspace reorder. Distinct group "ws" so a workspace drag can only
// target other workspace rows — never a nested terminal row (which carries a
// numeric workspace-id group).
const {
  dragIdx: wsDragIdx,
  overIdx: wsOverIdx,
  down: wsDragDown,
} = usePointerReorder((from, to) => store.reorderTopLevel(from, to));

function mimeForPath(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  if (ext === 'svg') return 'image/svg+xml';
  if (ext === 'ico') return 'image/x-icon';
  if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg';
  return 'image/png';
}

async function pickIcon(id: number) {
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: 'Image', extensions: ['png', 'jpg', 'jpeg', 'svg', 'ico'] }],
  });
  if (!selected || typeof selected !== 'string') return;
  const b64 = await invoke<string>('read_file_base64', { path: selected });
  const dataUrl = `data:${mimeForPath(selected)};base64,${b64}`;
  store.setIcon(id, dataUrl);
}

// Activate a terminal from the nested sidebar list. Switch to the workspace
// first if needed; its Terminal stays mounted while opened, so the request
// reaches it once active.
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

// ── context menu ───────────────────────────────────────────────────────────
const ctxMenu = ref<{ wsId: number; x: number; y: number } | null>(null);

function openCtxMenu(item: Workspace, e: MouseEvent) {
  // Clamp x so the menu (≈180px) never spills past the right edge.
  const x = Math.min(e.clientX, window.innerWidth - 190);
  ctxMenu.value = { wsId: item.id, x, y: e.clientY };
}
function ctxItem(): Workspace | undefined {
  return store.workspaces.find((w) => w.id === ctxMenu.value?.wsId);
}
function ctxOpen() {
  const w = ctxItem();
  ctxMenu.value = null;
  if (w) store.open(w);
}
function ctxRename() {
  const w = ctxItem();
  ctxMenu.value = null;
  if (w) startRename(w);
}
async function ctxIcon() {
  const id = ctxMenu.value?.wsId;
  ctxMenu.value = null;
  if (id != null) await pickIcon(id);
}
function ctxClearIcon() {
  const id = ctxMenu.value?.wsId;
  ctxMenu.value = null;
  if (id != null) store.clearIcon(id);
}
function ctxRemove() {
  const id = ctxMenu.value?.wsId;
  ctxMenu.value = null;
  if (id != null) store.remove(id);
}

// ── worktree context menu ────────────────────────────────────────────────────
const wtCtxMenu = ref<{ wtId: number; x: number; y: number } | null>(null);

function openWtCtxMenu(wt: Workspace, e: MouseEvent) {
  const x = Math.min(e.clientX, window.innerWidth - 200);
  wtCtxMenu.value = { wtId: wt.id, x, y: e.clientY };
}
function wtCtxItem(): Workspace | undefined {
  return store.workspaces.find((w) => w.id === wtCtxMenu.value?.wtId);
}
function wtCtxOpen() {
  const w = wtCtxItem();
  wtCtxMenu.value = null;
  if (w) store.open(w);
}
async function wtCtxRemove() {
  const id = wtCtxMenu.value?.wtId;
  wtCtxMenu.value = null;
  if (id == null) return;
  try {
    await store.removeWorktree(id);
  } catch (err) {
    // Likely uncommitted changes — offer a forced removal.
    const msg = err instanceof Error ? err.message : String(err);
    if (confirm(`Could not remove worktree:\n\n${msg}\n\nForce remove (discards uncommitted changes)?`)) {
      try { await store.removeWorktree(id, true); }
      catch (e2) { alert(`Force remove failed:\n${e2 instanceof Error ? e2.message : e2}`); }
    }
  }
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

function shortPath(p: string): string {
  const tilde = p.replace(/^\/Users\/[^/]+/, "~");
  const parts = tilde.split("/").filter(Boolean);
  if (parts.length <= 2) return tilde;
  return "~/" + parts.slice(-2).join("/");
}
</script>

<style scoped>
/* ── Sidebar shell ─────────────────────────────────────────────── */
.sidebar {
  width: var(--sidebar-width, 220px);
  flex: 0 0 var(--sidebar-width, 220px);
  background: var(--bg-panel);
  backdrop-filter: var(--blur-panels, none);
  -webkit-backdrop-filter: var(--blur-panels, none);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  overflow: hidden;
}

/* ── Header ────────────────────────────────────────────────────── */
.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 10px 8px 12px;
  flex-shrink: 0;
}

.header-title {
  display: flex;
  align-items: center;
  gap: 6px;
}

.header-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.header-count {
  font-size: 9px;
  font-weight: 600;
  color: var(--text-muted);
  background: var(--bg-hover);
  border-radius: 8px;
  padding: 1px 6px;
  line-height: 1.6;
}

.header-unread {
  font-size: 9px;
  font-weight: 700;
  color: #fff;
  background: var(--green);
  border-radius: 8px;
  padding: 1px 6px;
  line-height: 1.6;
  animation: pulse-unread 2s ease-in-out infinite;
}
@keyframes pulse-unread {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0.55; }
}

/* ── Icon buttons ──────────────────────────────────────────────── */
.icon-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 4px;
  border-radius: 5px;
  transition: color .12s, background .12s;
}
.icon-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
.icon-btn:active { transform: scale(0.9); }

/* ── Workspace list ────────────────────────────────────────────── */
.ws-list {
  flex: 1;
  overflow-y: auto;
  padding: 2px 0 6px;
}

.ws-group { margin-bottom: 2px; }

/* ── Workspace row ─────────────────────────────────────────────── */
.ws-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px 5px 7px;
  cursor: pointer;
  border-radius: 6px;
  margin: 0 4px;
  position: relative;
  transition: background .12s;
  touch-action: none;
}

.ws-item::before {
  content: "";
  position: absolute;
  left: 1px;
  top: 20%;
  height: 60%;
  width: 2px;
  border-radius: 1px;
  background: var(--accent);
  transform: scaleY(0);
  transform-origin: center;
  transition: transform .15s cubic-bezier(.2, .8, .2, 1);
}

.ws-item:hover { background: var(--bg-hover); }

.ws-item.active {
  background: color-mix(in srgb, var(--accent) 9%, transparent);
}
.ws-item.active::before { transform: scaleY(1); }

/* ── Caret ─────────────────────────────────────────────────────── */
.ws-caret {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  margin: 0 1px 0 -4px;
  flex-shrink: 0;
  border-radius: 4px;
  opacity: 0.55;
  transition: opacity .12s, color .12s, background .12s;
}
.ws-item:hover .ws-caret,
.ws-item.active .ws-caret { opacity: 0.85; }
.ws-caret:hover {
  opacity: 1 !important;
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
}

/* ── Workspace icon box ────────────────────────────────────────── */
.ws-icon-wrap {
  position: relative;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 6px;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
  transition: background .12s;
}
.ws-item.active .ws-icon-wrap {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
}
.ws-custom-icon { width: 24px; height: 24px; object-fit: cover; }
.ws-icon { color: var(--text-secondary); flex-shrink: 0; }
.ws-item.active .ws-icon { color: var(--accent); }

/* ── Workspace info ────────────────────────────────────────────── */
.ws-info {
  flex: 1;
  min-width: 0;
}

.ws-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  letter-spacing: -0.01em;
}

.ws-path {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 1px;
  opacity: 0.65;
}

/* ── Delete button ─────────────────────────────────────────────── */
.ws-delete {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: none;
  align-items: center;
  padding: 3px;
  border-radius: 4px;
  flex-shrink: 0;
  transition: color .12s, background .12s;
}
.ws-item:hover .ws-delete { display: flex; }
.ws-delete:hover { color: var(--red); background: color-mix(in srgb, var(--red) 12%, transparent); }

.ws-add-chat {
  position: relative;
  background: none;
  border: none;
  color: #d97706;
  cursor: pointer;
  display: none;
  align-items: center;
  padding: 3px;
  border-radius: 4px;
  flex-shrink: 0;
  transition: color .12s, background .12s;
}
.ws-item:hover .ws-add-chat { display: flex; }
.ws-add-chat:hover { background: color-mix(in srgb, #d97706 14%, transparent); }
.ws-add-chat-plus {
  position: absolute;
  right: 0;
  bottom: 0;
  color: var(--text);
}

/* ── Terminal tabs wrapper ─────────────────────────────────────── */
.ws-terminals {
  margin: 1px 6px 3px 26px;
  display: flex;
  flex-direction: column;
  gap: 1px;
  border-left: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  padding-left: 7px;
}
/* Chats continue the same left-rail list directly below the terminal tabs —
   no gap, no header, so they read as one list distinguished only by icon. */
.ws-chats { margin-top: -2px; }

/* ── Worktrees subsection ──────────────────────────────────────── */
.ws-worktrees {
  margin: 2px 6px 4px 26px;
  display: flex;
  flex-direction: column;
  gap: 1px;
  border-left: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  padding-left: 7px;
}

.ws-worktree-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 4px 2px 4px;
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--text-muted);
  opacity: 0.6;
}

.ws-worktree .ws-term-icon { color: #a78bfa; }
.ws-worktree.active .ws-term-icon { color: var(--accent); }
.claude-session-icon { color: #d97706; }
.ws-term.active .claude-session-icon { color: var(--accent); }

.ws-wt-terminals {
  margin: 1px 0 2px 12px;
  border-left-color: color-mix(in srgb, #a78bfa 28%, transparent);
}

/* ── Terminal / worktree tab row ───────────────────────────────── */
.ws-term {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 7px;
  border-radius: 5px;
  cursor: pointer;
  color: var(--text-secondary);
  position: relative;
  transition: background .12s, color .12s;
  touch-action: none;
}
.ws-term:hover { background: var(--bg-hover); color: var(--text-primary); }
.ws-term.active {
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  color: var(--text-primary);
}

.ws-term-icon { color: var(--text-muted); flex-shrink: 0; }
.ws-term-icon.agent { color: var(--accent); }
.ws-term.active .ws-term-icon { color: var(--accent); }

.ws-term-label {
  flex: 1;
  min-width: 0;
  font-size: 11.5px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ws-term-rename-input {
  flex: 1;
  min-width: 0;
  font-size: 11.5px;
  font-weight: 500;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--accent, #4ade80);
  outline: none;
  color: inherit;
  padding: 0;
  margin: 0;
  width: 100%;
}

.ws-term-split-count {
  flex-shrink: 0;
  min-width: 13px;
  height: 13px;
  padding: 0 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 9px;
  font-weight: 600;
  line-height: 1;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.08);
  color: var(--text-muted);
}

.ws-term-round {
  flex-shrink: 0;
  font-size: 9px;
  font-weight: 600;
  color: var(--text-muted);
  opacity: 0.7;
  min-width: 10px;
  text-align: right;
}

.ws-term-permission-bell {
  flex-shrink: 0;
  color: #f59e0b;
  opacity: 0.9;
}

/* Status dot styles in status-dots.css — no local overrides needed. */

/* ── PR badge ──────────────────────────────────────────────────── */
.pr-badge {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 9px;
  font-weight: 600;
  font-family: var(--font-mono);
  line-height: 1;
  padding: 1px 5px 1px 4px;
  border-radius: 7px;
  color: var(--text-muted);
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid transparent;
}
.pr-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--text-muted);
}
.pr-open    { color: #4ade80; background: color-mix(in srgb, #4ade80 12%, transparent); }
.pr-open    .pr-dot { background: #4ade80; }
.pr-draft   { color: #9ca3af; background: rgba(255, 255, 255, 0.06); }
.pr-draft   .pr-dot { background: #9ca3af; }
.pr-merged  { color: #a78bfa; background: color-mix(in srgb, #a78bfa 14%, transparent); }
.pr-merged  .pr-dot { background: #a78bfa; }
.pr-closed  { color: #f87171; background: color-mix(in srgb, #f87171 12%, transparent); }
.pr-closed  .pr-dot { background: #f87171; }
.pr-fail    { color: #f87171; background: color-mix(in srgb, #f87171 14%, transparent); }
.pr-fail    .pr-dot { background: #f87171; animation: pulse-unread 1.6s ease-in-out infinite; }
.pr-pending { color: #fbbf24; background: color-mix(in srgb, #fbbf24 14%, transparent); }
.pr-pending .pr-dot { background: #fbbf24; animation: pulse-unread 1.6s ease-in-out infinite; }

.ws-term-close {
  opacity: 0;
  color: var(--text-muted);
  flex-shrink: 0;
  border-radius: 3px;
  padding: 1px;
  transition: opacity .1s, color .1s;
}
.ws-term:hover .ws-term-close { opacity: 0.5; }
.ws-term-close:hover { opacity: 1 !important; color: var(--red); }

/* ── Drag states ───────────────────────────────────────────────── */
.ws-term.drag-over { background: var(--bg-hover); outline: 1px solid var(--accent); outline-offset: -1px; }
.ws-term.dragging { opacity: 0.4; }

.ws-item.drag-over { outline: 1px solid var(--accent); outline-offset: -1px; }
.ws-item.drag-over::after {
  content: "";
  position: absolute;
  left: 8px;
  right: 8px;
  top: -2px;
  height: 2px;
  border-radius: 2px;
  background: var(--accent);
}
.ws-item.dragging { opacity: 0.45; }

/* ── FLIP reorder animation ────────────────────────────────────── */
.ws-move-move { transition: transform .22s cubic-bezier(.2, .8, .2, 1); }

/* ── Empty state ───────────────────────────────────────────────── */
.ws-empty {
  font-size: 11.5px;
  color: var(--text-muted);
  text-align: center;
  padding: 36px 20px;
  line-height: 1.7;
  margin: 8px;
  border: 1px dashed color-mix(in srgb, var(--border) 60%, transparent);
  border-radius: 8px;
}

/* ── Footer ────────────────────────────────────────────────────── */
.sidebar-footer {
  border-top: 1px solid var(--border);
  padding: 6px 8px;
  flex-shrink: 0;
}

.footer-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  background: none;
  border: 1px solid color-mix(in srgb, var(--border) 65%, transparent);
  border-radius: 6px;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  font-weight: 500;
  padding: 6px 10px;
  transition: color .12s, border-color .12s, background .12s;
}
.footer-btn:hover {
  color: var(--text-secondary);
  border-color: var(--border);
  background: var(--bg-hover);
}
.footer-btn:active { transform: scale(0.985); }

/* ── Dialog overlay ────────────────────────────────────────────── */
.dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}

.dialog {
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 24px;
  width: 400px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.dialog h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}

.dialog-path {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-secondary);
  padding: 6px 8px;
  background: var(--bg-base);
  border-radius: 4px;
  border: 1px solid var(--border);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dialog-input {
  background: var(--bg-base);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  padding: 7px 10px;
  width: 100%;
}
.dialog-input:focus { border-color: var(--accent); }

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.btn-primary {
  display: flex;
  align-items: center;
  gap: 5px;
  background: var(--accent);
  border: none;
  border-radius: 5px;
  color: #fff;
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  padding: 6px 14px;
}
.btn-primary:hover:not(:disabled) { background: var(--accent-dim); }
.btn-primary:disabled { opacity: 0.5; cursor: default; }

.btn-secondary {
  display: flex;
  align-items: center;
  gap: 5px;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
  padding: 6px 14px;
}
.btn-secondary:hover { color: var(--text-primary); border-color: #444; }

/* ── Worktree dialog ───────────────────────────────────────────── */
.wt-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: -6px;
}
.wt-hint { font-weight: 400; color: var(--text-muted); }
.wt-error {
  font-size: 11px;
  color: var(--red);
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── Context menu ──────────────────────────────────────────────── */
.ctx-menu {
  position: fixed;
  z-index: 1000;
  min-width: 170px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 7px;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 1px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: none;
  border: none;
  border-radius: 4px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
  font-family: var(--font-ui);
  text-align: left;
  padding: 6px 10px;
}
.ctx-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.ctx-item.ctx-danger:hover { color: var(--red); }
.ctx-sep { height: 1px; background: var(--border); margin: 3px 0; }

/* ── Fleet strip ───────────────────────────────────────────────── */
.fleet-strip {
  margin: 0 6px 6px;
  border-radius: 7px;
  background: var(--bg-base);
  border: 1px solid var(--border);
  overflow: hidden;
  flex-shrink: 0;
}

.fleet-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px 4px;
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}

.fleet-header-icon { color: var(--accent); flex-shrink: 0; }

.fleet-count {
  margin-left: auto;
  font-size: 9px;
  font-weight: 700;
  background: color-mix(in srgb, var(--accent) 15%, transparent);
  color: var(--accent);
  border-radius: 8px;
  padding: 1px 6px;
  line-height: 1.6;
}

.fleet-row {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 8px;
  cursor: pointer;
  transition: background 0.1s;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
}
.fleet-row:last-child { border-bottom: none; }
.fleet-row:hover { background: var(--bg-hover); }
.fleet-row:hover .fleet-arrow { opacity: 0.6; }

.fleet-dot {
  flex-shrink: 0;
  width: 14px;
  text-align: center;
}

.fleet-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.fleet-tab {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fleet-ws {
  font-size: 9.5px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

.fleet-arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: 0;
  transition: opacity 0.1s;
}

/* ── Needs Attention strip ─────────────────────────────────────── */
.attention-strip {
  margin: 0 6px 6px;
  border-radius: 7px;
  background: var(--bg-base);
  border: 1px solid color-mix(in srgb, var(--red) 35%, var(--border));
  overflow: hidden;
  flex-shrink: 0;
}

.attention-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px 4px;
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}

.attention-header-icon { color: var(--red); flex-shrink: 0; }

.attention-count {
  margin-left: auto;
  font-size: 9px;
  font-weight: 700;
  background: color-mix(in srgb, var(--red) 15%, transparent);
  color: var(--red);
  border-radius: 8px;
  padding: 1px 6px;
  line-height: 1.6;
}

.attention-row {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 8px;
  cursor: pointer;
  transition: background 0.1s;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
}
.attention-row:last-child { border-bottom: none; }
.attention-row:hover { background: var(--bg-hover); }
.attention-row:hover .attention-arrow { opacity: 0.6; }

.attention-dot {
  flex-shrink: 0;
  width: 14px;
  text-align: center;
}

.attention-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.attention-tab {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.attention-ws {
  font-size: 9.5px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

.attention-arrow {
  flex-shrink: 0;
  color: var(--text-muted);
  opacity: 0;
  transition: opacity 0.1s;
}
</style>
