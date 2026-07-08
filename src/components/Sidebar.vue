<template>
  <aside class="sidebar">
    <!-- Workspace selector (repos + their worktrees) -->
    <WorkspacePicker
      @pick-folder="pickFolder"
      @rename="startRename"
      @new-worktree="openWtDialog"
    />

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

    <!-- One section per visible workspace: the pinned ones plus the active one -->
    <div class="ws-list">
      <div v-for="ws in sections" :key="ws.id" class="ws-section">
        <div class="tabs-header" :class="{ active: active?.id === ws.id }" @click="selectWs(ws)">
          <PhPushPin v-if="isPinned(ws.id)" :size="10" weight="fill" class="tabs-pin" />
          <PhGitBranch v-else-if="ws.parent_id" :size="11" class="tabs-pin wt" />
          <span class="tabs-label">{{ ws.worktree_branch || ws.name }}</span>
          <span v-if="tabCount(ws.id)" class="tabs-count">{{ tabCount(ws.id) }}</span>
          <span v-if="sections.length === 1 && unreadCount > 0" class="header-unread" :title="`${unreadCount} unread — ⌘⇧U to jump`">{{ unreadCount }}</span>
          <div class="tabs-actions">
            <button class="icon-btn" title="Board" @click.stop="ui.openBoard(ws.id)"><PhKanban :size="13" /></button>
            <button class="icon-btn chat" title="New chat" @click.stop="newChatSession(ws.id)"><ClaudeIcon :size="13" /></button>
            <button class="icon-btn" title="New terminal" @click.stop="termTabs.add(ws.id)"><PhPlus :size="13" /></button>
          </div>
        </div>

        <TransitionGroup name="ws-move" tag="div" class="ws-terminals">
          <div
            v-for="(tab, tabIdx) in termTabs.tabsByWs[ws.id] || []"
            :key="tab.id"
            class="ws-term"
            :data-reorder-idx="tabIdx"
            :data-reorder-group="String(ws.id)"
            :class="{
              active: active?.id === ws.id && termTabs.activeByWs[ws.id] === tab.id,
              'drag-over': tabDragGroup === String(ws.id) && tabOverIdx === tabIdx && tabDragIdx !== tabIdx,
              dragging: tabDragGroup === String(ws.id) && tabDragIdx === tabIdx,
            }"
            @click.stop="selectTab(ws, tab.id)"
            @dblclick.stop
            @pointerdown="(e: PointerEvent) => tabDragDown(tabIdx, e, String(ws.id))"
          >
            <ClaudeIcon v-if="tab.isChat" :size="11" class="ws-term-icon claude-session-icon" />
            <PhRobot v-else-if="tab.isAgent" :size="11" class="ws-term-icon agent" />
            <PhTerminal v-else :size="11" class="ws-term-icon" />
            <input
              v-if="editingTab?.wsId === ws.id && editingTab?.tabId === tab.id"
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
              @dblclick.stop="startTabRename(ws, tab)"
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
              @click.stop="termTabs.close(ws.id, tab.id)"
            />
          </div>
        </TransitionGroup>

        <div v-if="!tabCount(ws.id)" class="ws-empty sm">No tabs. Click + to open one.</div>
      </div>

      <div v-if="store.workspaces.length === 0" class="ws-empty">
        No workspaces.<br />Open a folder to start.
      </div>
    </div>

    <div class="sidebar-footer">
      <button class="footer-btn" @click="pickFolder">
        <PhFolderOpen :size="13" />
        Open Folder…
      </button>
    </div>
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
} from "@phosphor-icons/vue";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import WorkspacePicker from "@/components/WorkspacePicker.vue";
import { spinnerFrame } from "@/lib/spinner";
import { usePointerReorder } from "@/composables/usePointerReorder";
import { STATUS_PRIORITY, type TermStatus } from "@/lib/terminalStatus";
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

// Count of tabs with "review" status across ALL workspaces (agent finished while
// user wasn't watching). Drives the unread badge.
const unreadCount = computed(() => {
  let n = 0;
  for (const tabs of Object.values(termTabs.tabsByWs)) {
    n += tabs.filter((t) => t.status === "review").length;
  }
  return n;
});

// ── fleet view ────────────────────────────────────────────────────────────────
interface FleetItem { wsId: number; wsName: string; tabId: number; tabTitle: string; status: TermStatus; }

// statuses owned by the needs-attention strip; fleet skips them so a tab never
// renders twice
const ATTENTION_STATES = new Set<TermStatus>(["error", "permission", "waiting", "review"]);

const fleetItems = computed<FleetItem[]>(() => {
  const items: FleetItem[] = [];
  for (const ws of store.workspaces) {
    for (const tab of termTabs.tabsByWs[ws.id] ?? []) {
      // skip idle + anything the attention strip already shows (no dupes)
      if (tab.status !== "idle" && !ATTENTION_STATES.has(tab.status)) {
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

// Mount every workspace so its Terminal restores sessions into tabsByWs — the
// fleet/attention strips and the picker's status dots read from there, and the
// sidebar no longer has a per-workspace expand step to trigger it lazily.
// ponytail: mount-all. If a 20-repo setup gets slow, mount only the active repo
// + its worktrees and drive the other dots off the hook server instead.
function mountAll() {
  for (const ws of store.workspaces) store.ensureOpen(ws);
}

onMounted(() => {
  mountAll();
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
watch(() => store.workspaces.map(ws => ws.id).join(","), () => { mountAll(); refreshAllPrs(); });

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

/* ── Workspace section ─────────────────────────────────────────── */
.ws-section { margin-bottom: 4px; }

.tabs-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px 4px 10px;
  margin: 0 4px;
  border-radius: 5px;
  cursor: pointer;
  flex-shrink: 0;
  transition: background .12s;
}
.tabs-header:hover { background: var(--bg-hover); }
.tabs-header.active .tabs-label { color: var(--text-primary); }

.tabs-pin { color: var(--accent); flex-shrink: 0; }
.tabs-pin.wt { color: #a78bfa; }

.tabs-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tabs-count {
  font-size: 9px;
  font-weight: 600;
  color: var(--text-muted);
  background: var(--bg-hover);
  border-radius: 8px;
  padding: 1px 6px;
  line-height: 1.6;
}

.tabs-actions { margin-left: auto; display: flex; align-items: center; gap: 1px; opacity: 0; transition: opacity .12s; }
.tabs-header:hover .tabs-actions, .tabs-header.active .tabs-actions { opacity: 1; }

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
.icon-btn.chat { color: #d97706; }
.icon-btn.chat:hover { background: color-mix(in srgb, #d97706 14%, transparent); }

/* ── Tab list ──────────────────────────────────────────────────── */
.ws-list {
  flex: 1;
  overflow-y: auto;
  padding: 2px 0 6px;
}

.ws-terminals {
  margin: 1px 6px 3px 14px;
  border-left: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  padding-left: 6px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.claude-session-icon { color: #d97706; }
.ws-term.active .claude-session-icon { color: var(--accent); }

/* ── Terminal tab row ──────────────────────────────────────────── */
.ws-term {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 7px;
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

/* ── FLIP reorder animation ────────────────────────────────────── */
.ws-move-move { transition: transform .22s cubic-bezier(.2, .8, .2, 1); }

/* ── Empty state ───────────────────────────────────────────────── */
.ws-empty {
  font-size: 11.5px;
  color: var(--text-muted);
  text-align: center;
  padding: 30px 20px;
  line-height: 1.7;
  margin: 8px;
  border: 1px dashed color-mix(in srgb, var(--border) 60%, transparent);
  border-radius: 8px;
}
.ws-empty.sm { padding: 10px; margin: 2px 8px 4px 18px; font-size: 10.5px; border: none; opacity: 0.7; }

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
