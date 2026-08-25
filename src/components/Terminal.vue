<template>
  <div class="terminal-pane" @click="focusActive">
    <AgentToolbar @launch="spawnAgent" @open-chat="openClaudeChat()" @open-browser="openBrowserTab()" />

    <nav class="workspace-surface-switcher" aria-label="Workspace surface">
      <button
        class="surface-switch-btn"
        :class="{ active: activeSurface === 'chat' }"
        :aria-pressed="activeSurface === 'chat'"
        title="Show chat"
        @click.stop="switchSurface('chat')"
      >
        <PhChatCenteredText :size="13" />
        <span>Chat</span>
      </button>
      <button
        class="surface-switch-btn"
        :class="{ active: activeSurface === 'terminal' }"
        :aria-pressed="activeSurface === 'terminal'"
        title="Show terminal"
        @click.stop="switchSurface('terminal')"
      >
        <PhTerminal :size="13" />
        <span>Terminal</span>
      </button>
      <span class="surface-switch-rule" />
      <span class="surface-switch-context">{{ activeSurface === 'chat' ? 'Conversation' : 'Workspace' }}</span>
    </nav>

    <TransitionGroup v-if="tabs.length > 0" name="tab-move" tag="div" class="terminal-tabs">
      <button
        v-for="(tab, tabIdx) in tabs"
        :key="tab.id"
        class="tab"
        :class="{
          active: activeTabId === tab.id,
          'drag-over': tabOverIdx === tabIdx && tabDragIdx !== tabIdx,
          dragging: tabDragIdx === tabIdx,
        }"
        :data-reorder-idx="tabIdx"
        data-reorder-group="tab"
        :title="tabTooltip(tab)"
        @click.stop="activateTab(tab.id)"
        @pointerdown="(e: PointerEvent) => tabDragDown(tabIdx, e, 'tab')"
      >
        <PhFileCode v-if="tabIsEditor(tab)" :size="12" class="tab-term-icon" />
        <PhChatCenteredText v-else-if="tabIsChat(tab)" :size="12" class="tab-chat-icon" />
        <PhGlobe v-else-if="tabIsBrowser(tab)" :size="12" class="tab-term-icon" />
        <PhRobot v-else-if="tabIsAgent(tab)" :size="12" class="tab-agent-icon" />
        <PhTerminal v-else :size="12" class="tab-term-icon" />
        <span v-if="tabIsEditor(tab) && tabDirty(tab)" class="dirty-dot" />
        <span
          v-else-if="tabStatus(tab) !== 'idle'"
          class="status-dot"
          :class="`status-${tabStatus(tab)}`"
          :title="tabStatus(tab) === 'error' ? tabStatusDetail(tab) : undefined"
        >{{ tabStatus(tab) === 'running' ? spinnerFrame : '' }}</span>
        <span class="tab-label" :class="{ 'tab-flash': getAllLeaves(tab.root).some(l => flashingLeafs.has(l.id)) }">{{ tabTitle(tab) }}</span>
        <span v-if="tabStatusText(tab)" class="tab-status-text">{{ tabStatusText(tab) }}</span>
        <span
          v-if="tabProgress(tab) !== undefined"
          class="tab-progress-bar"
          :title="tabProgressLabel(tab)"
        >
          <span class="tab-progress-fill" :style="{ width: `${(tabProgress(tab)! * 100).toFixed(0)}%` }" />
        </span>
        <span
          v-if="tabLeafCount(tab) > 1"
          class="tab-split-count"
          :title="`${tabLeafCount(tab)} panes`"
        >{{ tabLeafCount(tab) }}</span>
        <PhArrowSquareOut
          :size="10"
          class="tab-float"
          title="Pop out as floating window"
          data-no-drag
          @click.stop="popOutTab(tab)"
        />
        <PhX
          :size="10"
          weight="bold"
          class="tab-close"
          data-no-drag
          @click.stop="closeTab(tab.id)"
        />
      </button>
      <button key="__add" class="tab tab-add" @click="addTab()" title="New terminal">
        <PhPlus :size="12" />
      </button>

    </TransitionGroup>

    <!-- Log strip: last entries from `burrow log` for the active tab -->
    <TransitionGroup
      v-if="activeTabLogs.length"
      name="log-fade"
      tag="div"
      class="log-strip"
    >
      <div
        v-for="entry in activeTabLogs"
        :key="entry.ts"
        class="log-entry"
        :class="`log-${entry.level}`"
      >
        <span class="log-level">{{ entry.level }}</span>
        <span class="log-msg">{{ entry.message }}</span>
      </div>
    </TransitionGroup>

    <div
      v-if="activeAgentLeafId !== null && historyStore.getTimeline(activeAgentLeafId).length > 0"
      class="agent-timeline-strip"
      :class="{ collapsed: timelineCollapsed }"
    >
      <div class="tl-strip-header" @click="timelineCollapsed = !timelineCollapsed">
        <component :is="timelineCollapsed ? PhCaretRight : PhCaretDown" :size="9" weight="bold" />
        <span>Timeline</span>
        <span class="tl-strip-turns">{{ historyStore.getTimeline(activeAgentLeafId).length }}t</span>
      </div>
      <div v-if="!timelineCollapsed" class="tl-strip-body">
        <AgentTimeline :pty-id="activeAgentLeafId" />
      </div>
    </div>

    <AgentPlanRail
      v-if="activeAgentLeafId !== null"
      :pty-id="activeAgentLeafId"
    />

    <div v-if="tabs.length > 0" class="terminal-body">
      <div
        v-for="tab in tabs"
        :key="tab.id"
        class="terminal-tab-content"
        v-show="activeTabId === tab.id"
      >
        <div
          v-for="pane in paneLayout(tab)"
          :key="pane.leaf.id"
          class="pane"
          :class="{ focused: focusedLeafId === pane.leaf.id && isTabSplit(tab) }"
          :style="rectStyle(pane.rect)"
          :data-leaf-id="pane.leaf.id"
          @mousedown.capture="onLeafFocus(pane.leaf.id)"
        >
          <template v-if="splitDragActive">
            <div class="drop-zone dz-left"   :class="{ 'dz-active': hoveredZone?.leafId === pane.leaf.id && hoveredZone?.dir === 'h' && hoveredZone?.side === 'first' }" />
            <div class="drop-zone dz-right"  :class="{ 'dz-active': hoveredZone?.leafId === pane.leaf.id && hoveredZone?.dir === 'h' && hoveredZone?.side === 'second' }" />
            <div class="drop-zone dz-top"    :class="{ 'dz-active': hoveredZone?.leafId === pane.leaf.id && hoveredZone?.dir === 'v' && hoveredZone?.side === 'first' }" />
            <div class="drop-zone dz-bottom" :class="{ 'dz-active': hoveredZone?.leafId === pane.leaf.id && hoveredZone?.dir === 'v' && hoveredZone?.side === 'second' }" />
          </template>
          <div v-if="isTabSplit(tab)" class="pane-titlebar" @mousedown.stop>
            <PhFileCode v-if="pane.leaf.leafType === 'editor'" :size="10" class="pane-title-icon" />
            <PhGlobe v-else-if="pane.leaf.leafType === 'browser'" :size="10" class="pane-title-icon" />
            <PhRobot v-else-if="pane.leaf.isAgent" :size="10" class="pane-title-icon agent" />
            <PhTerminal v-else :size="10" class="pane-title-icon" />
            <span v-if="pane.leaf.leafType === 'editor' && pane.leaf.dirty" class="dirty-dot" />
            <span
              v-else-if="pane.leaf.status !== 'idle'"
              class="status-dot"
              :class="`status-${pane.leaf.status}`"
              :title="pane.leaf.status === 'error' ? pane.leaf.statusDetail : undefined"
            >{{ pane.leaf.status === 'running' ? spinnerFrame : '' }}</span>
            <span class="pane-title-text">{{ pane.leaf.title }}</span>
            <button class="pane-title-close" @click.stop="closePane(pane.leaf.id)" title="Close pane">
              <PhX :size="9" weight="bold" />
            </button>
          </div>
          <DiffTab
            v-if="pane.leaf.leafType === 'diff'"
            :diff-file="pane.leaf.diffFile!"
            :diff-staged="pane.leaf.diffStaged ?? false"
            :diff="pane.leaf.diff || ''"
            :feedback-target-pty-id="pane.leaf.diffOwnerPtyId"
            :send-feedback="(payload) => sendDiffFeedback(pane.leaf.diffOwnerPtyId, payload)"
          />
          <CodeEditor
            v-else-if="pane.leaf.leafType === 'editor'"
            :leaf-id="pane.leaf.id"
            :path="pane.leaf.filePath!"
            :cwd="pane.leaf.cwd ?? cwd"
            :ref="(el) => registerLeaf(pane.leaf.id, el)"
            @title="(t) => onLeafTitle(pane.leaf.id, t)"
            @dirty="(d) => onLeafDirty(pane.leaf.id, d)"
            @saved="() => onLeafSaved(pane.leaf.id)"
            @error="(m) => onLeafError(m)"
          />
          <ClaudeChat
            v-else-if="pane.leaf.leafType === 'chat'"
            :chat-id="pane.leaf.chatId!"
            :workspace-id="workspaceId"
            :cwd="pane.leaf.cwd ?? cwd"
            :is-watching="isWatching(tab)"
          />
          <BrowserPane
            v-else-if="pane.leaf.leafType === 'browser'"
            :initial-url="pane.leaf.browserUrl"
          />
          <XTerm
            v-else
            :pty-id="pane.leaf.id"
            :cwd="pane.leaf.cwd ?? cwd"
            :initial-cmd="pane.leaf.initialCmd"
            :result-token="pane.leaf.resultToken"
            :initially-titled="!isDefaultTitle(pane.leaf.title)"
            :ref="(el) => registerLeaf(pane.leaf.id, el)"
            @title="(t) => onLeafTitle(pane.leaf.id, t)"
            @busy="(b) => onLeafBusy(pane.leaf.id, b)"
            @agent="(b) => onLeafAgent(pane.leaf.id, b)"
            @agent-state="(s, d) => onAgentState(pane.leaf.id, s, d)"
            @agent-meta="(m) => onAgentMeta(pane.leaf.id, m)"
            @needs-input="(b) => onLeafNeedsInput(pane.leaf.id, b)"
            @interrupt="() => onLeafInterrupt(pane.leaf.id)"
            @spawn="(req) => addTab(req.cmd, { cwd: req.cwd || undefined, resultToken: req.token || undefined })"
            @cwd="(p) => onLeafCwd(pane.leaf.id, p)"
          />
        </div>
        <div
          v-for="(div, i) in paneDividers(tab)"
          :key="`div-${i}`"
          class="pane-divider"
          :style="dividerStyle(div)"
          @mousedown.stop.prevent="startDividerDrag($event, div)"
        />
      </div>
    </div>
    <div v-else class="terminal-welcome">
      <PhTerminalWindow :size="40" weight="thin" class="welcome-icon" />
      <p class="welcome-title">No terminals open</p>
      <p class="welcome-sub">Launch an agent above or open a new terminal</p>
      <button class="welcome-btn" @click="addTab()">
        <PhPlus :size="13" /> New Terminal
      </button>
    </div>

    <div v-if="confirm" class="confirm-overlay" @mousedown.self="answerClose(false)">
      <div class="confirm-modal">
        <div class="confirm-title">{{ confirm.reason === 'unsaved' ? 'Unsaved changes' : 'Close terminal' }}</div>
        <div class="confirm-body">
          "{{ confirm.name }}" {{ confirm.reason === 'unsaved' ? 'has unsaved changes' : 'has a running process' }}. Close anyway?
        </div>
        <div class="confirm-actions">
          <button class="confirm-btn" @click="answerClose(false)">Cancel</button>
          <button class="confirm-btn danger" @click="answerClose(true)">Close <span class="confirm-kbd">⌘↵</span></button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { createActor, type Actor } from "xstate";
import { agentStatusMachine, isBusyStatus } from "@/machines/agentStatus";
import { PhRobot, PhTerminal, PhTerminalWindow, PhX, PhPlus, PhArrowSquareOut, PhFileCode, PhGlobe, PhCaretDown, PhCaretRight, PhChatCenteredText } from "@phosphor-icons/vue";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import XTerm from "./XTerm.vue";
import DiffTab from "./DiffTab.vue";
import CodeEditor from "./CodeEditor.vue";
import ClaudeChat from "./ClaudeChat.vue";
import BrowserPane from "./BrowserPane.vue";
import AgentToolbar from "./AgentToolbar.vue";
import { type Leaf, type TreeNode, type SplitNode } from "./TerminalSplitView.vue";
import { nextPtyId, initPtyCounter } from "@/lib/ptyId";
import { spinnerFrame } from "@/lib/spinner";
import { playSound } from "@/lib/sounds";
import { notifyNtfy } from "@/lib/ntfy";
import type { NtfyEvent } from "@/stores/ui";
import {
  aggregateStatus,
  deriveTabTitle,
  isDefaultTitle,
  type TermStatus,
} from "@/lib/terminalStatus";
import { useAgentHistoryStore } from "@/stores/agentHistory";
import AgentTimeline from "@/components/AgentTimeline.vue";
import AgentPlanRail from "@/components/AgentPlanRail.vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useBoardTasksStore } from "@/stores/boardTasks";
import { useNotificationsStore } from "@/stores/notifications";
import { useGitStore } from "@/stores/git";
import { usePointerReorder } from "@/composables/usePointerReorder";
import { useDragSplit, type SplitZone } from "@/composables/useDragSplit";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import { useDiagram } from "@/composables/useDiagram";

const props = defineProps<{ cwd: string; workspaceId: number }>();
const wsStore = useWorkspaceStore();
const uiStore = useUIStore();
const { showDiagram } = useDiagram();
const chatsStore = useClaudeChatsStore();
const tabsStore = useTerminalTabsStore();
const boardTasksStore = useBoardTasksStore();
const notifStore = useNotificationsStore();
const gitStore = useGitStore();
const historyStore = useAgentHistoryStore();

interface Tab {
  id: number;
  root: TreeNode;
}

interface PersistedTab {
  /** The live (meaningful) display title — agent-set or last command name. */
  title: string | null;
  /** The "Terminal N" base fallback, separate from the live title. */
  default_title: string | null;
  initial_cmd: string | null;
  pty_id: number | null;
  cwd: string | null;
  session_id: string | null;
}

interface LogEntry {
  level: "info" | "warn" | "error";
  message: string;
  ts: number;
}

interface DaemonSession {
  pty_id: number;
  cwd: string;
  title: string;
  alive: boolean;
}

const tabs = ref<Tab[]>([]);
const activeTabId = ref(0);
const focusedLeafId = ref(0);
const activeSurface = computed<"chat" | "terminal">(() => {
  const active = tabs.value.find((tab) => tab.id === activeTabId.value);
  return active && tabIsChat(active) ? "chat" : "terminal";
});
// Holds both XTerm and CodeEditor instances, keyed by leaf id. Both expose
// focus(); editor leaves also expose save()/isDirty(), terminal leaves sendText().
const xtermRefs = new Map<number, any>();
let terminalCounter = 0;

function registerLeaf(id: number, el: unknown) {
  if (el) xtermRefs.set(id, el);
  else xtermRefs.delete(id);
}

// ── layout ──────────────────────────────────────────────────────────────────
// Flatten the split tree into absolutely-positioned panes (in %). Splitting
// only changes these rects, so existing XTerm instances/PTYs are reused — no
// remount, no blink.
interface Rect { left: number; top: number; width: number; height: number; }
// A draggable boundary between two sibling panes. `node` is the split it resizes
// (drag mutates node.ratio); `nodeRect` is that split's full area (for mapping the
// pointer back to a ratio); `dir` follows the split direction.
interface Divider { node: SplitNode; dir: "h" | "v"; rect: Rect; nodeRect: Rect; }

function paneLayout(tab: Tab): { leaf: Leaf; rect: Rect }[] {
  const out: { leaf: Leaf; rect: Rect }[] = [];
  walkLayout(tab.root, { left: 0, top: 0, width: 100, height: 100 }, out, []);
  return out;
}

function paneDividers(tab: Tab): Divider[] {
  const divs: Divider[] = [];
  walkLayout(tab.root, { left: 0, top: 0, width: 100, height: 100 }, [], divs);
  return divs;
}

function walkLayout(
  node: TreeNode,
  rect: Rect,
  out: { leaf: Leaf; rect: Rect }[],
  divs: Divider[],
) {
  if (node.type === "leaf") {
    out.push({ leaf: node, rect });
    return;
  }
  const r = node.ratio ?? 0.5;
  if (node.direction === "h") {
    const w = rect.width * r;
    walkLayout(node.first, { ...rect, width: w }, out, divs);
    walkLayout(node.second, { ...rect, left: rect.left + w, width: rect.width - w }, out, divs);
    divs.push({
      node, dir: "h", nodeRect: rect,
      rect: { left: rect.left + w, top: rect.top, width: 0, height: rect.height },
    });
  } else {
    const h = rect.height * r;
    walkLayout(node.first, { ...rect, height: h }, out, divs);
    walkLayout(node.second, { ...rect, top: rect.top + h, height: rect.height - h }, out, divs);
    divs.push({
      node, dir: "v", nodeRect: rect,
      rect: { left: rect.left, top: rect.top + h, width: rect.width, height: 0 },
    });
  }
}

function rectStyle(r: Rect) {
  // 1px insets create thin gaps that show the body background as dividers.
  return {
    left: `calc(${r.left}% + ${r.left > 0 ? 1 : 0}px)`,
    top: `calc(${r.top}% + ${r.top > 0 ? 1 : 0}px)`,
    width: `calc(${r.width}% - ${r.left > 0 ? 1 : 0}px)`,
    height: `calc(${r.height}% - ${r.top > 0 ? 1 : 0}px)`,
  };
}

// A 7px-wide hit zone centered on the boundary; the visible 1px line is the gap
// behind it. The handle straddles the seam so the cursor target is generous.
function dividerStyle(d: Divider) {
  if (d.dir === "h") {
    return {
      left: `${d.rect.left}%`, top: `${d.rect.top}%`,
      width: "7px", height: `${d.rect.height}%`,
      transform: "translateX(-50%)", cursor: "col-resize",
    };
  }
  return {
    left: `${d.rect.left}%`, top: `${d.rect.top}%`,
    width: `${d.rect.width}%`, height: "7px",
    transform: "translateY(-50%)", cursor: "row-resize",
  };
}

// ── divider drag ──────────────────────────────────────────────────────────────
let dragDiv: { node: SplitNode; dir: "h" | "v"; nodeRect: Rect; container: HTMLElement } | null = null;

function startDividerDrag(e: MouseEvent, d: Divider) {
  const container = (e.currentTarget as HTMLElement).closest(".terminal-tab-content") as HTMLElement | null;
  if (!container) return;
  dragDiv = { node: d.node, dir: d.dir, nodeRect: d.nodeRect, container };
  e.preventDefault();
  window.addEventListener("mousemove", onDividerMove);
  window.addEventListener("mouseup", endDividerDrag);
  document.body.style.userSelect = "none";
}

function onDividerMove(e: MouseEvent) {
  if (!dragDiv) return;
  // getBoundingClientRect and clientX share the #app-zoom visual space, so the
  // ratio is correct under any UI scale (both scale together, cancel out).
  const box = dragDiv.container.getBoundingClientRect();
  const { node, dir, nodeRect } = dragDiv;
  let ratio: number;
  if (dir === "h") {
    const pct = ((e.clientX - box.left) / box.width) * 100;
    ratio = (pct - nodeRect.left) / nodeRect.width;
  } else {
    const pct = ((e.clientY - box.top) / box.height) * 100;
    ratio = (pct - nodeRect.top) / nodeRect.height;
  }
  node.ratio = Math.max(0.08, Math.min(0.92, ratio));
}

function endDividerDrag() {
  dragDiv = null;
  window.removeEventListener("mousemove", onDividerMove);
  window.removeEventListener("mouseup", endDividerDrag);
  document.body.style.userSelect = "";
}

// ── tree helpers ────────────────────────────────────────────────────────────

function findLeaf(node: TreeNode, id: number): Leaf | null {
  if (node.type === "leaf") return node.id === id ? node : null;
  return findLeaf(node.first, id) || findLeaf(node.second, id);
}

function getFirstLeaf(node: TreeNode): Leaf {
  if (node.type === "leaf") return node;
  return getFirstLeaf(node.first);
}

function getAllLeaves(node: TreeNode): Leaf[] {
  if (node.type === "leaf") return [node];
  return [...getAllLeaves(node.first), ...getAllLeaves(node.second)];
}

function removeLeaf(node: TreeNode, id: number): TreeNode | null {
  if (node.type === "leaf") return node.id === id ? null : node;
  const first = removeLeaf(node.first, id);
  const second = removeLeaf(node.second, id);
  if (!first) return second;
  if (!second) return first;
  return { ...node, first, second };
}

function insertSplit(
  node: TreeNode,
  targetId: number,
  direction: "h" | "v",
  newNode: TreeNode,
  side: "first" | "second" = "second",
): TreeNode {
  if (node.type === "leaf") {
    if (node.id === targetId)
      return side === "second"
        ? { type: "split", direction, first: node, second: newNode, ratio: 0.5 }
        : { type: "split", direction, first: newNode, second: node, ratio: 0.5 };
    return node;
  }
  return {
    ...node,
    first: insertSplit(node.first, targetId, direction, newNode, side),
    second: insertSplit(node.second, targetId, direction, newNode, side),
  };
}

function containsLeaf(node: TreeNode, id: number): boolean {
  if (node.type === "leaf") return node.id === id;
  return containsLeaf(node.first, id) || containsLeaf(node.second, id);
}

// ── tab helpers ─────────────────────────────────────────────────────────────

function tabTitle(tab: Tab): string {
  const leaves = getAllLeaves(tab.root);
  const focused = activeTabId.value === tab.id
    ? findLeaf(tab.root, focusedLeafId.value) ?? undefined
    : undefined;
  return deriveTabTitle(leaves, focused);
}

function tabIsAgent(tab: Tab): boolean {
  const leaves = getAllLeaves(tab.root);
  const focused = activeTabId.value === tab.id
    ? findLeaf(tab.root, focusedLeafId.value) ?? undefined
    : undefined;
  return (focused ?? leaves[0])?.isAgent ?? false;
}

function isChat(tab: Tab): boolean {
  return tabIsChat(tab);
}

// A single-leaf editor tab shows a file icon + dirty dot instead of a status dot.
function tabIsEditor(tab: Tab): boolean {
  return tab.root.type === "leaf" && tab.root.leafType === "editor";
}

function tabIsChat(tab: Tab): boolean {
  return tab.root.type === "leaf" && tab.root.leafType === "chat";
}

function tabIsBrowser(tab: Tab): boolean {
  return tab.root.type === "leaf" && tab.root.leafType === "browser";
}

function tabDirty(tab: Tab): boolean {
  return getAllLeaves(tab.root).some((l) => l.leafType === "editor" && l.dirty);
}

// ── Per-leaf hook-server event listeners ─────────────────────────────────────
// Keyed by ptyId. Registered when a leaf is created, cleaned up when closed.
const leafUnlisteners = new Map<number, UnlistenFn[]>();
// XState actors for agent status — one per terminal leaf.
const leafActors = new Map<number, Actor<typeof agentStatusMachine>>();
const flashingLeafs = ref(new Set<number>());
// Log strip: last N entries per tab (keyed by tab.id, NOT leaf.id).
const tabLogs = ref<Record<number, LogEntry[]>>({});

function findTabIdByLeafId(leafId: number): number | null {
  for (const tab of tabs.value) {
    if (findLeaf(tab.root, leafId)) return tab.id;
  }
  return null;
}

/** Locate a leaf and its owning tab. The status machine's actions need both at fire time. */
function locateLeaf(leafId: number): { tab: Tab; leaf: Leaf } | null {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, leafId);
    if (leaf) return { tab, leaf };
  }
  return null;
}

/** A turn settled (done or review): toast + OS notification + ntfy + git refresh. */
function onTurnSettled(leafId: number) {
  const found = locateLeaf(leafId);
  if (!found) return;
  notifyDone(found.leaf.title, found.tab.id);
  maybeNtfy("done", found.leaf.title);
  if (gitStore.cwd === props.cwd) gitStore.refresh(true);
}

function registerLeafListeners(leafId: number) {
  // XState actor — the SOLE owner of leaf.status. Every channel (hooks, foreground
  // poll, interrupt/watchdog) sends it events; nothing else writes leaf.status.
  // Side effects are machine actions, so a transition and its sound/notification
  // can never drift apart.
  const actor = createActor(
    agentStatusMachine.provide({
      actions: {
        playWaiting: () => playSound("waiting"),
        onDone: () => onTurnSettled(leafId),
        onReview: () => { playSound("done"); onTurnSettled(leafId); },
        onError: () => maybeNtfy("error", locateLeaf(leafId)?.leaf.title ?? ""),
      },
    }),
    { input: {} },
  );
  actor.subscribe((snapshot) => {
    const leaf = locateLeaf(leafId)?.leaf;
    if (!leaf) return;
    leaf.status = snapshot.value as TermStatus;
    leaf.statusDetail = snapshot.context.detail;
    leaf.busy = isBusyStatus(leaf.status);
    // Mirror to disk so `burrow list-tabs` / MCP `list_tabs` (pure Rust/DB
    // reads, no frontend round-trip) can report whether the agent actually
    // finished instead of just pty id + title.
    invoke("set_tab_live_status", { ptyId: leafId, status: leaf.status }).catch(() => {});
  });
  actor.start();
  leafActors.set(leafId, actor);

  const unlisteners: UnlistenFn[] = [];
  Promise.all([
    listen<string>(`pty-status-text-${leafId}`, (ev) => {
      for (const tab of tabs.value) {
        const leaf = findLeaf(tab.root, leafId);
        if (leaf) { leaf.statusText = ev.payload || undefined; break; }
      }
    }),
    listen(`pty-flash-${leafId}`, () => {
      flashingLeafs.value = new Set(flashingLeafs.value).add(leafId);
      setTimeout(() => {
        const next = new Set(flashingLeafs.value);
        next.delete(leafId);
        flashingLeafs.value = next;
      }, 600);
    }),
    listen<{ diff: string; title: string }>(`pty-open-diff-${leafId}`, (ev) => {
      const { diff, title } = ev.payload;
      if (diff) openDiffInTab(title, false, diff, leafId);
    }),
    listen<{ progress: number | null; label: string }>(`pty-progress-${leafId}`, (ev) => {
      for (const tab of tabs.value) {
        const leaf = findLeaf(tab.root, leafId);
        if (leaf) {
          leaf.progress = ev.payload.progress ?? undefined;
          leaf.progressLabel = ev.payload.label || undefined;
          break;
        }
      }
    }),
    listen<{ level: string; message: string }>(`pty-log-${leafId}`, (ev) => {
      const tabId = findTabIdByLeafId(leafId);
      if (tabId === null) return;
      const entry: LogEntry = {
        level: (ev.payload.level as LogEntry["level"]) || "info",
        message: ev.payload.message,
        ts: Date.now(),
      };
      const prev = tabLogs.value[tabId] ?? [];
      tabLogs.value = { ...tabLogs.value, [tabId]: [...prev, entry].slice(-20) };
    }),
    listen<string>(`pty-session-id-${leafId}`, (ev) => {
      for (const tab of tabs.value) {
        const leaf = findLeaf(tab.root, leafId);
        if (leaf) { leaf.sessionId = ev.payload; break; }
      }
    }),
  ]).then((fns) => {
    // Only store if the leaf wasn't already removed before promises resolved.
    if (leafUnlisteners.has(leafId)) {
      fns.forEach((fn) => leafUnlisteners.get(leafId)!.push(fn));
    } else {
      fns.forEach((fn) => fn());
    }
  });
  leafUnlisteners.set(leafId, unlisteners);
}

function unregisterLeafListeners(leafId: number) {
  leafUnlisteners.get(leafId)?.forEach((fn) => fn());
  leafUnlisteners.delete(leafId);
  const actor = leafActors.get(leafId);
  if (actor) { actor.stop(); leafActors.delete(leafId); }
}

function makeLeaf(initialCmd?: string, extra?: { cwd?: string; resultToken?: string; id?: number; taskId?: string }): Leaf {
  terminalCounter++;
  return {
    type: "leaf",
    id: extra?.id ?? nextPtyId(),
    title: `Terminal ${terminalCounter}`,
    defaultTitle: `Terminal ${terminalCounter}`,
    isAgent: false,
    busy: false,
    status: "idle",
    initialCmd,
    cwd: extra?.cwd,
    resultToken: extra?.resultToken,
    taskId: extra?.taskId,
  };
}

// ── events from split tree ──────────────────────────────────────────────────

function onLeafFocus(id: number) {
  focusedLeafId.value = id;
  nextTick(() => xtermRefs.get(id)?.focus());
}

function onLeafTitle(id: number, title: string) {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, id);
    if (!leaf) continue;
    // Empty string → no-op (sticky names: a transient shell-foreground poll
    // must not wipe a meaningful title). Only a real non-empty title updates the
    // leaf. A stray leading robot emoji (older seeds) is stripped.
    if (!title) break;
    leaf.title = title.replace(/^🤖\s*/, "");
    break;
  }
}

// Whether this leaf is currently running an agent — driven by the foreground
// poll (authoritative), independent of the title text.
function onLeafAgent(id: number, isAgent: boolean) {
  const leaf = locateLeaf(id)?.leaf;
  if (!leaf) return;
  leaf.isAgent = isAgent;
  // The machine gates its poll channel on this: once a leaf is an agent, only
  // hooks may drive its status.
  leafActors.get(id)?.send({ type: "SET_AGENT", isAgent });
}

// OSC 7 CWD update: shell emits \e]7;file://host/path\a after each `cd`.
// Update the leaf's live cwd so `burrow spawn` and restore get accurate paths.
function onLeafCwd(id: number, cwd: string) {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, id);
    if (!leaf) continue;
    leaf.cwd = cwd;
    break;
  }
}

// busy comes from the foreground-process poll only — NOT from OSC titles
// (the shell sets the title to the cwd, which must not count as "running").
// The machine's `notAgent` guard drops these for agent leaves: hooks are the
// sole status authority there.
function onLeafBusy(id: number, busy: boolean) {
  const found = locateLeaf(id);
  if (!found) return;
  leafActors.get(id)?.send(
    busy ? { type: "BUSY" } : { type: "NOT_BUSY", watching: isWatching(found.tab) },
  );
}

// True when the user is actively looking at this tab: its workspace is the
// visible one, this tab is the active tab, and the window has OS focus.
function isWatching(tab: Tab): boolean {
  return (
    uiStore.mode === "terminal" &&
    wsStore.active?.id === props.workspaceId &&
    activeTabId.value === tab.id &&
    document.hasFocus()
  );
}

// Mark every finished leaf in a tab as seen (user opened/returned to it).
function markTabSeen(tab: Tab) {
  for (const leaf of getAllLeaves(tab.root)) {
    // MARK_SEEN is only handled in done/review/error — a no-op elsewhere.
    leafActors.get(leaf.id)?.send({ type: "MARK_SEEN" });
  }
}

// The agent's hook state (running | waiting | done), forwarded verbatim from
// XTerm. ONE semantic event → one clean transition via the XState actor, so a
// trailing "waiting" can never clobber a fresh "done".
function onAgentState(id: number, s: string, detail?: string) {
  const found = locateLeaf(id);
  if (!found) return;
  const { tab, leaf } = found;
  // Track turn count before sending (subscription updates leaf.status after send).
  if (s === "running" && leaf.status !== "running") {
    leaf.round = (leaf.round ?? 0) + 1;
    // A board task runs in its own worktree by default. Capture its baseline
    // before the agent starts work; non-board terminals intentionally remain
    // un-attributed instead of falling back to the global Git diff.
    if (leaf.taskId) {
      invoke("begin_agent_turn", {
        taskId: leaf.taskId,
        ptyId: leaf.id,
        worktreePath: leaf.cwd ?? props.cwd,
      }).catch(() => {});
    }
  }
  if ((s === "done" || s === "error") && leaf.taskId) {
    invoke("complete_agent_turn", { ptyId: leaf.id, state: s }).catch(() => {});
  }
  historyStore.addEvent(leaf.id, s);
  const actor = leafActors.get(id);
  if (!actor) return;
  if (s === "running") actor.send({ type: "START" });
  else if (s === "waiting") actor.send({ type: "WAIT" });
  else if (s === "permission") actor.send({ type: "PERMISSION_REQUEST" });
  else if (s === "done") actor.send({ type: "STOP", watching: isWatching(tab) });
  else if (s === "error") actor.send({ type: "FAIL", detail: detail || undefined });
}

// SessionStart metadata (model + title) — NOT a status. Stash the model for an
// unobtrusive tab tooltip; prefer the session title only over a generic
// "Terminal N" default so an agent-set descriptive title is never clobbered.
function onAgentMeta(id: number, meta: { model: string; source: string; title: string }) {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, id);
    if (!leaf) continue;
    if (meta.model) leaf.model = meta.model;
    if (meta.title) {
      leaf.sessionTitle = meta.title;
      if (isDefaultTitle(leaf.title)) leaf.title = meta.title;
    }
    break;
  }
}

// Push an ntfy.sh notification for an agent transition, gated by the
// Integrations settings (enabled, topic set, event subscribed, away-only).
function maybeNtfy(event: NtfyEvent, leafTitle: string) {
  if (!uiStore.ntfyEnabled || !uiStore.ntfyTopic) return;
  if (!uiStore.ntfyEvents.includes(event)) return;
  if (uiStore.ntfyOnlyWhenAway && document.hasFocus()) return;
  notifyNtfy(
    { server: uiStore.ntfyServer, topic: uiStore.ntfyTopic, token: uiStore.ntfyToken || undefined },
    event,
    leafTitle || "Agent",
  ).catch(() => {}); // best-effort: a failed push must never disrupt the UI
}

async function notifyDone(leafTitle: string, tabId?: number) {
  const toastTitle = "Task complete";
  const body = leafTitle || "Agent finished";
  notifStore.push({ type: "done", title: toastTitle, body, workspaceId: props.workspaceId, tabId });
  // System notification when window not focused.
  // Title = "Burrow" so the app name is visible even in dev mode
  // (where macOS shows the terminal emulator name instead of the bundle name).
  if (!document.hasFocus()) {
    let granted = await isPermissionGranted();
    if (!granted) {
      const perm = await requestPermission();
      granted = perm === "granted";
    }
    if (granted) sendNotification({ title: "Burrow", body: `✓ ${body}` });
  }
}

// User pressed ESC / Ctrl+C in the PTY — an agent interrupt. Agents emit no Stop
// hook when a turn is cancelled and the foreground poll never clears an agent's
// "running" (it stays foreground at its prompt), so without this the dot sticks
// orange. The turn was CANCELLED, not completed → settle straight to idle (no
// "done"/"review" badge, no sound). Only act on a live running/waiting leaf so a
// stray ESC at an idle prompt is a harmless no-op.
function onLeafInterrupt(id: number) {
  // Only handled in running/waiting/permission — a stray ESC at an idle prompt
  // is a no-op by construction.
  leafActors.get(id)?.send({ type: "INTERRUPT" });
}

// Output-buffer heuristic from the poll (plain commands only — the machine's
// `notAgent` guard drops it for agent leaves).
function onLeafNeedsInput(id: number, needs: boolean) {
  leafActors.get(id)?.send({ type: "NEEDS_INPUT", needs });
}

function tabStatus(tab: Tab): TermStatus {
  return aggregateStatus(getAllLeaves(tab.root), (l) => l.status);
}

function tabStatusText(tab: Tab): string {
  for (const l of getAllLeaves(tab.root)) {
    if (l.statusText) return l.statusText;
  }
  return "";
}

// Error cause (rate_limit|overloaded|…) for the red dot's tooltip.
function tabStatusDetail(tab: Tab): string {
  for (const l of getAllLeaves(tab.root)) {
    if (l.status === "error" && l.statusDetail) return l.statusDetail;
  }
  return "";
}

// Unobtrusive tab tooltip: agent model (from SessionStart) + any error cause.
function tabTooltip(tab: Tab): string | undefined {
  const parts: string[] = [];
  for (const l of getAllLeaves(tab.root)) {
    if (l.model) { parts.push(`Model: ${l.model}`); break; }
  }
  const detail = tabStatusDetail(tab);
  if (detail) parts.push(`Error: ${detail}`);
  return parts.length ? parts.join(" · ") : undefined;
}

function tabProgress(tab: Tab): number | undefined {
  for (const l of getAllLeaves(tab.root)) {
    if (l.progress !== undefined) return l.progress;
  }
  return undefined;
}

function tabProgressLabel(tab: Tab): string {
  for (const l of getAllLeaves(tab.root)) {
    if (l.progressLabel) return l.progressLabel;
  }
  return "";
}

const activeTabLogs = computed(() => {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return [];
  return (tabLogs.value[tab.id] ?? []).slice(-5);
});

// ── in-app close confirmation ───────────────────────────────────────────────

const confirm = ref<{ name: string; reason: "running" | "unsaved"; resolve: (ok: boolean) => void } | null>(null);

function confirmClose(name: string, reason: "running" | "unsaved" = "running"): Promise<boolean> {
  return new Promise((resolve) => {
    confirm.value = { name, reason, resolve };
    window.addEventListener("keydown", onConfirmKey);
  });
}

function answerClose(ok: boolean) {
  window.removeEventListener("keydown", onConfirmKey);
  confirm.value?.resolve(ok);
  confirm.value = null;
}

function onConfirmKey(e: KeyboardEvent) {
  if (!confirm.value) return;
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
    e.preventDefault();
    answerClose(true);
  } else if (e.key === "Escape") {
    e.preventDefault();
    answerClose(false);
  }
}

// ── tab management ──────────────────────────────────────────────────────────

function activateTab(id: number) {
  activeTabId.value = id;
  const tab = tabs.value.find((t) => t.id === id);
  if (!tab) return;
  // User is now looking at this tab — clear any done/review badge.
  markTabSeen(tab);
  const leaf = getFirstLeaf(tab.root);
  focusedLeafId.value = leaf.id;
  nextTick(() => xtermRefs.get(leaf.id)?.focus());
}

function openClaudeChat(chatId?: number, agentId?: string, cwd?: string) {
  let session: import("@/stores/claudeChats").ClaudeSession;
  if (chatId != null) {
    session = chatsStore.sessions.find((s) => s.id === chatId) ?? chatsStore.create(props.workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent });
  } else {
    session = chatsStore.create(props.workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent });
  }
  // Focus existing tab if already open
  const existing = tabs.value.find(
    (t) => t.root.type === "leaf" && t.root.leafType === "chat" && (t.root as Leaf).chatId === session.id
  );
  if (existing) { activateTab(existing.id); return; }
  // Create new chat tab
  const id = nextPtyId();
  const leaf: Leaf = {
    type: "leaf",
    id,
    title: session.title,
    defaultTitle: session.title,
    isAgent: false,
    busy: false,
    status: "idle",
    leafType: "chat",
    chatId: session.id,
    cwd: cwd || undefined,
  };
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activateTab(tab.id);
}

function switchSurface(surface: "chat" | "terminal") {
  if (activeSurface.value === surface) return;
  const matchingTab = tabs.value.find((tab) => surface === "chat" ? tabIsChat(tab) : !tabIsChat(tab));
  if (matchingTab) {
    activateTab(matchingTab.id);
    return;
  }
  if (surface === "chat") openClaudeChat();
  else addTab();
}

function openBrowserTab(url?: string) {
  const id = nextPtyId();
  const leaf: Leaf = {
    type: "leaf",
    id,
    title: (() => { try { return url ? new URL(url.startsWith("http") ? url : `https://${url}`).hostname : "Browser"; } catch { return url ?? "Browser"; } })(),
    defaultTitle: "Browser",
    isAgent: false,
    busy: false,
    status: "idle",
    leafType: "browser",
    browserUrl: url,
  };
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activateTab(tab.id);
}

function addTab(initialCmd?: string, extra?: { cwd?: string; resultToken?: string; background?: boolean; taskId?: string }): Leaf {
  const leaf = makeLeaf(initialCmd, extra);
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  registerLeafListeners(leaf.id);
  // `background` keeps focus on the current tab (e.g. `burrow spawn` sub-agents
  // open on the side without yanking the user away). PTY still spawns on mount.
  if (!extra?.background) {
    activeTabId.value = tab.id;
    focusedLeafId.value = leaf.id;
    nextTick(() => xtermRefs.get(leaf.id)?.focus());
  }
  return leaf;
}

function spawnAgent(cmd: string) {
  addTab(cmd);
}

// Adopt an already-running daemon PTY (e.g. a Mission Control task handed off to a
// real terminal tab). The leaf's id IS the existing ptyId, so XTerm's create_pty
// hits the daemon's idempotent "session alive → reattach" path: no new process, the
// ring buffer replays, and the live agent continues here. No initialCmd — nothing is
// typed; we're attaching, not launching. If a tab already owns this PTY, just focus
// it (re-handoff is a no-op rather than a double-attach).
function adoptPty(opts: { ptyId: number; cwd: string; title: string; sessionId?: string }): Leaf {
  for (const tab of tabs.value) {
    const existing = findLeaf(tab.root, opts.ptyId);
    if (existing) { focusLeaf(opts.ptyId); return existing; }
  }
  const leaf = makeLeaf(undefined, { cwd: opts.cwd, id: opts.ptyId });
  leaf.isAgent = true;
  leaf.title = opts.title;
  leaf.defaultTitle = opts.title;
  leaf.sessionId = opts.sessionId;
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activeTabId.value = tab.id;
  focusedLeafId.value = leaf.id;
  registerLeafListeners(leaf.id);
  nextTick(() => xtermRefs.get(leaf.id)?.focus());
  return leaf;
}

// Move a tab from one position to another (shared by the top tab-bar drag and the
// reorder request coming back from the Sidebar). syncStore (deep watch on tabs)
// mirrors the new order to the store, so the Sidebar list follows automatically.
function reorderTabs(from: number, to: number) {
  if (from < 0 || from >= tabs.value.length || to < 0 || to >= tabs.value.length) return;
  const moved = tabs.value.splice(from, 1)[0];
  if (moved) tabs.value.splice(to, 0, moved);
}

function onSplit(fromIdx: number, zone: SplitZone) {
  const srcTab = tabs.value[fromIdx];
  if (!srcTab) return;
  const targetTab = tabs.value.find((t) => containsLeaf(t.root, zone.leafId));
  if (!targetTab || srcTab === targetTab) return;

  targetTab.root = insertSplit(targetTab.root, zone.leafId, zone.dir, srcTab.root, zone.side);
  tabs.value.splice(fromIdx, 1);
  if (activeTabId.value === srcTab.id) activeTabId.value = targetTab.id;
}

const {
  active: splitDragActive,
  hoveredZone,
  activate: activateSplitDrag,
} = useDragSplit({ onSplit });

// Pointer-based drag reorder for the top tab bar (HTML5 DnD is swallowed by
// Tauri's native handler — see usePointerReorder). Group "tab" so a drag can only
// land on another tab button. Dragging downward past the tab bar switches to
// split-drop mode via onEscape.
const {
  dragIdx: tabDragIdx,
  overIdx: tabOverIdx,
  down: tabDragDown,
} = usePointerReorder((from, to) => reorderTabs(from, to), {
  onEscape: (idx, e) => activateSplitDrag(idx, e, tabTitle(tabs.value[idx])),
});

// Inject a file/folder path from the explorer into the focused leaf's PTY as an
// "@path " context reference (relative to the workspace cwd when possible) so the
// active agent picks it up. User reviews + hits Enter.
function insertContext(absPath: string) {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return;
  let rel = absPath;
  const base = props.cwd.replace(/\/+$/, "");
  if (base && absPath.startsWith(base + "/")) rel = absPath.slice(base.length + 1);
  const ref = `@${rel} `;
  const xterm = xtermRefs.get(focusedLeafId.value);
  xterm?.sendText(ref);
}

function sendDiffFeedback(
  ownerPtyId: number | undefined,
  payload: { comment: string; selectedDiff: string },
): Promise<boolean> {
  if (ownerPtyId === undefined) return Promise.resolve(false);
  const owner = locateLeaf(ownerPtyId)?.leaf;
  if (!owner?.isAgent) return Promise.resolve(false);

  const context = payload.selectedDiff
    ? `\n\nSelected diff context:\n${payload.selectedDiff}`
    : "";
  const text = `[Review feedback]\n${payload.comment}${context}\n`;
  return invoke("write_pty", {
    id: ownerPtyId,
    data: Array.from(new TextEncoder().encode(text)),
  }).then(() => true).catch(() => false);
}

function openDiffInTab(file: string, staged: boolean, diff: string, ownerPtyId?: number) {
  terminalCounter++;
  const leaf: Leaf = {
    type: "leaf",
    id: nextPtyId(),
    title: `Diff: ${file}`,
    defaultTitle: `Diff: ${file}`,
    isAgent: false,
    busy: false,
    status: "idle",
    leafType: "diff",
    diffFile: file,
    diffStaged: staged,
    diff,
    diffOwnerPtyId: ownerPtyId,
  };
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activeTabId.value = tab.id;
}

// ── editor leaves ─────────────────────────────────────────────────────────────

function onLeafDirty(id: number, dirty: boolean) {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, id);
    if (!leaf) continue;
    leaf.dirty = dirty;
    break;
  }
}

function onLeafSaved(_id: number) {
  // Saving likely changed the working tree — refresh git panel if it's showing
  // this workspace's repo.
  if (gitStore.cwd === props.cwd) gitStore.refresh(true);
}

function onLeafError(msg: string) {
  notifStore.push({ type: "error", title: "Editor", body: msg });
}

// Prefer the live editor instance (authoritative), fall back to the leaf flag.
function isLeafDirty(leaf: Leaf): boolean {
  const ref = xtermRefs.get(leaf.id);
  if (ref?.isDirty) return ref.isDirty();
  return !!leaf.dirty;
}

// Open a file from the explorer as an editor tab beside the terminal tabs. If the
// file is already open anywhere, focus it instead of duplicating.
function openFileInTab(path: string, name: string) {
  for (const tab of tabs.value) {
    const existing = getAllLeaves(tab.root).find(
      (l) => l.leafType === "editor" && l.filePath === path,
    );
    if (existing) {
      activeTabId.value = tab.id;
      markTabSeen(tab);
      focusedLeafId.value = existing.id;
      nextTick(() => xtermRefs.get(existing.id)?.focus());
      return;
    }
  }
  const id = nextPtyId();
  const leaf: Leaf = {
    type: "leaf",
    id,
    title: name,
    defaultTitle: name,
    isAgent: false,
    busy: false,
    status: "idle",
    leafType: "editor",
    filePath: path,
    dirty: false,
  };
  const tab: Tab = { id, root: leaf };
  tabs.value.push(tab);
  activeTabId.value = id;
  focusedLeafId.value = id;
  nextTick(() => xtermRefs.get(id)?.focus());
}

function splitFocused(direction: "h" | "v") {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return;
  const newLeaf = makeLeaf();
  tab.root = insertSplit(tab.root, focusedLeafId.value, direction, newLeaf);
  focusedLeafId.value = newLeaf.id;
  registerLeafListeners(newLeaf.id);
  nextTick(() => xtermRefs.get(newLeaf.id)?.focus());
}

async function closeTab(tabId: number) {
  const tab = tabs.value.find((t) => t.id === tabId);
  if (!tab) return;

  const leaves = getAllLeaves(tab.root);
  const busyLeaf = leaves.find((l) => l.busy);
  if (busyLeaf) {
    const ok = await confirmClose(busyLeaf.title);
    if (!ok) return;
  }
  const dirtyLeaf = leaves.find((l) => l.leafType === "editor" && isLeafDirty(l));
  if (dirtyLeaf) {
    const ok = await confirmClose(dirtyLeaf.title, "unsaved");
    if (!ok) return;
  }

  // Explicitly kill PTYs so the daemon drops them (not a detach — user closed the
  // tab). Editor/diff leaves have no PTY — skip them. AWAIT each kill: fire-and-forget
  // raced app-quit — if the user closed a tab then quit before the daemon processed
  // the kill, the session survived alive and reattached+replayed its ring buffer on
  // next launch ("old session in a new terminal"). Awaiting guarantees the kill lands
  // before we drop the tab and re-persist.
  await Promise.all(
    leaves
      .filter((l) => l.leafType !== "editor" && l.leafType !== "diff" && l.leafType !== "chat")
      .map((l) => {
        const p = invoke("kill_pty", { id: l.id }).catch(() => {});
        unregisterLeafListeners(l.id);
        historyStore.clear(l.id);
        return p;
      }),
  );
  // Chat leaves have no PTY — stop their adapter/CLI on explicit close instead.
  leaves.filter((l) => l.leafType === "chat").forEach(stopChatSession);

  const idx = tabs.value.findIndex((t) => t.id === tabId);
  tabs.value.splice(idx, 1);

  // Persist NOW rather than waiting on the reactive watcher's next tick — same
  // app-quit race: the SQLite DELETE+INSERT must land so the closed tab can't be
  // restored on next launch.
  persist();

  if (activeTabId.value === tabId && tabs.value.length > 0) {
    const newTab = tabs.value[Math.max(0, idx - 1)];
    activateTab(newTab.id);
  }
}

function isTabSplit(tab: Tab): boolean {
  return tab.root.type === 'split';
}

function tabLeafCount(tab: Tab): number {
  return getAllLeaves(tab.root).length;
}

// Pull the task prompt out of a `burrow spawn` command line for chat-mode spawns.
// burrow re-quotes args single-quoted, so the prompt is the last quoted chunk;
// fall back to everything after the program name. ponytail: naive quote scan,
// good enough for `claude 'task'` / `claude --model x 'task'`.
function spawnPrompt(cmd: string): string {
  const quoted = [...cmd.matchAll(/'([^']*)'/g)].map((m) => m[1]);
  if (quoted.length) return quoted[quoted.length - 1];
  return cmd.replace(/^\S+\s*/, "").trim();
}

// Explicit teardown of a chat leaf's backend adapter/CLI. Called ONLY on user
// close (closeTab/closePane) — ClaudeChat.onBeforeUnmount deliberately no longer
// stops the proc, so a background remount can't kill a live agent. Transport is
// per-session; a wrong-map stop is a harmless no-op, but resolve it to be clean.
function stopChatSession(leaf: Leaf) {
  if (leaf.chatId == null) return;
  const sess = chatsStore.sessions.find((s) => s.id === leaf.chatId);
  const cmd = sess?.transport === "acp" ? "acp_stop" : "claude_stop";
  invoke(cmd, { id: leaf.chatId }).catch(() => {});
}

async function closePane(leafId: number) {
  const tab = tabs.value.find((t) => findLeaf(t.root, leafId));
  if (!tab) return;
  const leaves = getAllLeaves(tab.root);
  if (leaves.length === 1) {
    await closeTab(tab.id);
    return;
  }
  const leaf = findLeaf(tab.root, leafId);
  if (leaf?.busy) {
    const ok = await confirmClose(leaf.title);
    if (!ok) return;
  }
  if (leaf?.leafType === "editor" && isLeafDirty(leaf)) {
    const ok = await confirmClose(leaf.title, "unsaved");
    if (!ok) return;
  }
  if (leaf?.leafType === "chat") {
    stopChatSession(leaf);
  } else if (leaf && leaf.leafType !== "editor" && leaf.leafType !== "diff") {
    // Await the kill (see closeTab) so it lands before re-persist / app-quit.
    await invoke("kill_pty", { id: leafId }).catch(() => {});
    unregisterLeafListeners(leafId);
    historyStore.clear(leafId);
  }
  const newRoot = removeLeaf(tab.root, leafId)!;
  tab.root = newRoot;
  persist();
  if (focusedLeafId.value === leafId) {
    const remaining = getAllLeaves(newRoot);
    focusedLeafId.value = remaining[0].id;
    nextTick(() => xtermRefs.get(remaining[0].id)?.focus());
  }
}

function focusActive() {
  xtermRefs.get(focusedLeafId.value)?.focus();
}

function onKeydown(e: KeyboardEvent) {
  if (wsStore.active?.id !== props.workspaceId) return;
  if (e.ctrlKey && !e.metaKey && !e.altKey && /^[1-9]$/.test(e.key)) {
    e.preventDefault();
    const idx = parseInt(e.key) - 1;
    const wsTabs = tabsStore.tabsByWs[props.workspaceId] ?? [];
    if (wsTabs[idx]) activateTab(wsTabs[idx].id);
    return;
  }
  if (e.metaKey && e.shiftKey && !e.ctrlKey && !e.altKey && e.key === "U") {
    // Jump to first unread (review) tab in this workspace.
    const reviewTab = tabs.value.find((t) => tabStatus(t) === "review");
    if (reviewTab) {
      e.preventDefault();
      activateTab(reviewTab.id);
    }
    return;
  }
  if (!e.metaKey || e.ctrlKey || e.altKey) return;
  const k = e.key.toLowerCase();
  if (k === "t") {
    e.preventDefault();
    addTab();
  } else if (k === "w") {
    e.preventDefault();
    closePane(focusedLeafId.value);
  } else if (k === "d") {
    e.preventDefault();
    splitFocused(e.shiftKey ? "v" : "h");
  }
}

// ── persistence ─────────────────────────────────────────────────────────────

function allLeaves(): Leaf[] {
  return tabs.value.flatMap((t) => getAllLeaves(t.root));
}

const CHAT_TABS_KEY = (wsId: number) => `burrow.chat_tabs.${wsId}`;

function persist() {
  // Editor leaves have no PTY — don't persist them as bogus pty rows. Restoring
  // open editors on restart is a follow-up.
  const payload: PersistedTab[] = allLeaves()
    .filter((l) => l.leafType !== "editor" && l.leafType !== "chat")
    .map((l) => ({
    title: l.title,           // live meaningful title (agent-set, command name, …)
    default_title: l.defaultTitle,  // "Terminal N" fallback
    initial_cmd: l.initialCmd ?? null,
    pty_id: l.id,
    cwd: l.cwd ?? null,
    session_id: l.sessionId ?? null,
  }));
  invoke("save_terminal_tabs", { workspaceId: props.workspaceId, tabs: payload });

  // Persist open chat tab chatIds separately (no PTY, so not in SQLite).
  const chatIds = allLeaves()
    .filter((l) => l.leafType === "chat" && l.chatId != null)
    .map((l) => l.chatId!);
  try {
    localStorage.setItem(CHAT_TABS_KEY(props.workspaceId), JSON.stringify(chatIds));
  } catch {}
}

// Include title, defaultTitle, and sessionId so any of those changes triggers a save.
watch(
  () => allLeaves().map((l) => `${l.id}|${l.title}|${l.defaultTitle}|${l.sessionId ?? ""}`).join(","),
  persist,
);

// ── sidebar mirror ──────────────────────────────────────────────────────────
// Push tab summaries to the shared store so the Sidebar can list terminals
// nested under this workspace, and react to clicks coming back from it.

function syncStore() {
  tabsStore.setTabs(
    props.workspaceId,
    tabs.value.map((t) => ({
      id: t.id,
      title: tabTitle(t),
      isAgent: tabIsAgent(t),
      isChat: isChat(t),
      busy: getAllLeaves(t.root).some((l) => l.busy),
      status: tabStatus(t),
      leafCount: getAllLeaves(t.root).length,
      round: Math.max(0, ...getAllLeaves(t.root).map((l) => l.round ?? 0)),
      taskId: getAllLeaves(t.root)[0]?.taskId,
      sessionId: getAllLeaves(t.root)[0]?.sessionId,
    })),
  );
  tabsStore.setActive(props.workspaceId, activeTabId.value);
}

watch([tabs, activeTabId, focusedLeafId], syncStore, { deep: true });

// Sync chat session status + title → leaf so the Sidebar dot, tab-bar dot,
// and tab label stay live. Chat leaves have no PTY events, so both must flow from the store.
watch(
  () => chatsStore.sessions.map((s) => ({ id: s.id, status: s.status, title: s.title })),
  (sessions) => {
    for (const sess of sessions) {
      for (const tab of tabs.value) {
        const leaf = getAllLeaves(tab.root).find(
          (l) => l.leafType === "chat" && l.chatId === sess.id,
        );
        if (!leaf) continue;
        if (leaf.status !== (sess.status ?? "idle")) {
          leaf.status = sess.status ?? "idle";
        }
        if (sess.title && leaf.title !== sess.title) {
          leaf.title = sess.title;
        }
      }
    }
  },
  { deep: true },
);

// When the user switches TO this workspace (with the window focused), treat its
// active tab as seen so a "review" badge earned while it was hidden clears.
watch(
  () => wsStore.active?.id,
  (id) => {
    if (id !== props.workspaceId || !document.hasFocus()) return;
    const tab = tabs.value.find((t) => t.id === activeTabId.value);
    if (tab) markTabSeen(tab);
  },
);

// Returning to terminal mode (from dashboard/git/mission) counts as seeing the
// active tab — clear any "review" badge it earned while a non-terminal mode was up.
watch(
  () => uiStore.mode,
  (m) => {
    if (m !== "terminal" || wsStore.active?.id !== props.workspaceId || !document.hasFocus()) return;
    const tab = tabs.value.find((t) => t.id === activeTabId.value);
    if (tab) markTabSeen(tab);
  },
);

// Requests arrive through a single shared slot. A workspace the sidebar just
// switched to has no mounted Terminal yet (or is still restoring), so the watch
// never fires for it — onMounted replays the pending request. The nonce guard
// keeps that replay from re-running one this instance already handled.
let handledNonce = 0;

function applyTabRequest(req: typeof tabsStore.request) {
  if (!req || req.wsId !== props.workspaceId || req.nonce === handledNonce) return;
  // An activate for a tab that hasn't been restored yet stays unhandled, so the
  // onMounted replay picks it up once the tab list exists.
  if (req.action === "activate" && !tabs.value.some((t) => t.id === req.tabId)) return;
  handledNonce = req.nonce;
  if (req.action === "activate" && req.tabId != null) activateTab(req.tabId);
  else if (req.action === "add") addTab(req.cmd || undefined, { taskId: req.taskId });
  else if (req.action === "close" && req.tabId != null) closeTab(req.tabId);
  else if (req.action === "reorder" && req.fromIdx != null && req.toIdx != null) {
    reorderTabs(req.fromIdx, req.toIdx);
  }
  else if (req.action === "openChat") openClaudeChat(req.chatId, req.agentId);
  else if (req.action === "rename" && req.tabId != null && req.title != null) {
    const tab = tabs.value.find((t) => t.id === req.tabId);
    if (tab) getAllLeaves(tab.root).forEach((l) => { l.title = req.title!; });
  }
}

watch(() => tabsStore.request, applyTabRequest);

onMounted(async () => {
  window.addEventListener("keydown", onKeydown);

  const [saved, daemonSessions] = await Promise.all([
    invoke<PersistedTab[]>("list_terminal_tabs", { workspaceId: props.workspaceId }),
    invoke<DaemonSession[]>("list_pty_sessions").catch(() => [] as DaemonSession[]),
  ]);

  // Build set of alive PTY ids from daemon for quick lookup
  const alivePtys = new Set(daemonSessions.filter((s) => s.alive).map((s) => s.pty_id));

  // Advance the global pty-id counter past EVERY id the daemon still knows about
  // (alive or not) AND every saved tab — not just saved tabs. A tab closed in a
  // past session leaves its PTY in the daemon but drops out of `saved`; if the
  // counter only cleared the saved max, a fresh tab could be handed that orphan's
  // id and `create_pty` would re-attach to the dead/closed session instead of
  // starting clean. Done unconditionally so a workspace with no saved tabs but
  // live orphan sessions still can't collide.
  // Exclude Mission Control's offset id-space (>= PTY_BASE = 2_000_000): those are
  // a separate range on purpose, and counting them would shove normal tabs up into
  // the MC range. Anything below MC_RANGE_START is a normal tab id.
  const MC_RANGE_START = 1_000_000;
  const maxDaemonId = daemonSessions.reduce(
    (m, s) => (s.pty_id != null && s.pty_id < MC_RANGE_START ? Math.max(m, s.pty_id) : m),
    0,
  );
  const maxSavedId = saved.reduce((m, s) => Math.max(m, s.pty_id ?? 0), 0);
  initPtyCounter(Math.max(maxSavedId, maxDaemonId));

  if (saved.length) {
    saved.forEach((s) => {
      // Use saved pty_id when the session is alive in daemon, otherwise get fresh id
      const useSavedId = s.pty_id != null && alivePtys.has(s.pty_id);
      // Auto-resume Claude if PTY is dead but we have a session_id.
      // Pattern: `claude --resume <id>` — picks up the conversation where it left off.
      const resumeCmd =
        !useSavedId &&
        s.session_id &&
        /\bclaude\b/.test(s.initial_cmd ?? "")
          ? `claude --resume ${s.session_id}`
          : undefined;
      const leaf = makeLeaf(resumeCmd, {
        cwd: s.cwd ?? undefined,
        id: useSavedId ? s.pty_id! : undefined,
      });
      // Restore the "Terminal N" base title (defaultTitle), then the live title.
      // Old rows with no default_title fall back to the auto-generated counter name.
      leaf.defaultTitle = s.default_title || leaf.defaultTitle;
      // Restore the live meaningful title (agent-set, command name, …).
      // Falls back to defaultTitle if not saved (old rows / first-run).
      leaf.title = s.title || leaf.defaultTitle;
      if (s.session_id) leaf.sessionId = s.session_id;
      const tab: Tab = { id: leaf.id, root: leaf };
      tabs.value.push(tab);
      registerLeafListeners(leaf.id);
    });
    activateTab(tabs.value[0].id);
  }

  // Restore chat tabs saved from the previous session.
  try {
    const raw = localStorage.getItem(CHAT_TABS_KEY(props.workspaceId));
    if (raw) {
      const chatIds: number[] = JSON.parse(raw);
      for (const chatId of chatIds) {
        // openClaudeChat skips if session no longer exists (creates a fresh one),
        // and skips if tab is already open — safe to call unconditionally.
        const sessionExists = chatsStore.sessions.some((s) => s.id === chatId);
        if (sessionExists) openClaudeChat(chatId);
      }
    }
  } catch {}

  syncStore();
  // Replay a request that landed while this Terminal was mounting/restoring —
  // e.g. the sidebar clicking a tab of a workspace that wasn't open yet.
  applyTabRequest(tabsStore.request);
});

// Handle a `burrow worktree` request: create a git worktree off this workspace's
// repo and let the store's reload surface it in the Sidebar (nested under the repo).
// The parent must be a top-level repo — if this PTY runs inside a worktree, climb to
// its parent so we never try to make a worktree of a worktree (the Rust command
// rejects that anyway). Path matches the New-worktree dialog: <worktreesDir>/<repo>/<branch>.
async function handleWorktreeRequest(branch: string, base: string) {
  const self = wsStore.workspaces.find((w) => w.id === props.workspaceId);
  const parentId = self?.parent_id ?? props.workspaceId;
  const parent = wsStore.workspaces.find((w) => w.id === parentId) ?? self;
  const repo = (parent?.path.split("/").filter(Boolean).pop()) || "repo";
  const path = `${uiStore.worktreesDir}/${repo}/${branch}`;
  try {
    const ws = await wsStore.createWorktree(parentId, branch, base.trim() || null, path);
    wsStore.open(ws);
  } catch (err) {
    console.error("burrow worktree request failed:", err);
  }
}

// Poll for `burrow spawn` requests routed to this workspace (file-based, since
// agents' Bash/hooks have no controlling tty for the OSC channel).
let spawnPoll: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  spawnPoll = setInterval(async () => {
    try {
      const reqs = await invoke<
        { kind: string; cmd: string; token: string; cwd: string; branch: string; base: string; tmuxWin: string; wsid: string; tabid: string; content: string }[]
      >("take_spawn_requests", { cwd: props.cwd });
      for (const r of reqs) {
        if (r.kind === "diagram") {
          showDiagram(r.content);
        } else if (r.kind === "worktree") {
          await handleWorktreeRequest(r.branch, r.base);
        } else if (r.kind === "focus-workspace") {
          // Switch Burrow to (and mount) the requested workspace. wsStore is a shared
          // singleton, so the origin Terminal can drive the global active workspace.
          const ws = wsStore.workspaces.find((w) => w.id === Number(r.wsid));
          if (ws) wsStore.open(ws);
        } else if (r.kind === "focus-tab") {
          // Activate a tab by its (pty) id, switching to its owning workspace first.
          const tabId = Number(r.tabid);
          let ownerWs: number | undefined;
          for (const [wid, list] of Object.entries(tabsStore.tabsByWs)) {
            if (list.some((t) => t.id === tabId)) { ownerWs = Number(wid); break; }
          }
          if (ownerWs !== undefined) {
            if (wsStore.active?.id !== ownerWs) {
              const ws = wsStore.workspaces.find((w) => w.id === ownerWs);
              if (ws) wsStore.open(ws);
            }
            tabsStore.activate(ownerWs, tabId);
          }
        } else if (r.kind === "new-tab") {
          // Open a new tab in the target workspace (default: this one). Same-workspace
          // is handled directly; a different workspace is opened then messaged via the
          // shared tabs store so its own Terminal creates the tab.
          const targetWs = r.wsid ? Number(r.wsid) : props.workspaceId;
          if (targetWs === props.workspaceId) {
            addTab(r.cmd || undefined);
          } else {
            const ws = wsStore.workspaces.find((w) => w.id === targetWs);
            if (ws) wsStore.open(ws);
            tabsStore.add(targetWs, r.cmd || undefined);
          }
        } else if (r.kind === "tab-rename") {
          // Rename a tab by its (pty) id — find its owning (mounted) workspace and
          // route through the tabs store, same as the Sidebar rename. r.cmd = title.
          const tabId = Number(r.tabid);
          const title = r.cmd;
          for (const [wid, list] of Object.entries(tabsStore.tabsByWs)) {
            if (list.some((t) => t.id === tabId)) { tabsStore.rename(Number(wid), tabId, title); break; }
          }
        } else if (r.kind === "tab-close") {
          // Close a tab by its (pty) id via the owning workspace's tabs store.
          const tabId = Number(r.tabid);
          for (const [wid, list] of Object.entries(tabsStore.tabsByWs)) {
            if (list.some((t) => t.id === tabId)) { tabsStore.close(Number(wid), tabId); break; }
          }
        } else if (r.kind === "board-move") {
          // Frontend half of `burrow board-move <taskId> todo` (lib.rs's
          // take_spawn_requests board-move arm pushes this instead of answering
          // purely in Rust, since the Backlog→Todo transition needs worktree
          // creation + agent spawn — client-only logic). r.wsid carries the task
          // id (reusing the generic field slot); r.tabid is always "todo" here
          // (every other column is answered in Rust with no frontend involved).
          // The claiming Terminal (ws === cwd, i.e. this workspace) is the task's
          // repo — that's where the Manager runs `burrow board-move` from.
          const taskId = r.wsid;
          try {
            await boardTasksStore.load(props.workspaceId);
            const task = (boardTasksStore.tasksByRepo[props.workspaceId] || []).find((t) => t.id === taskId);
            if (task && task.board_column === "backlog") await boardTasksStore.startTask(task);
          } catch (err) {
            console.error("burrow board-move (todo) failed:", err);
          }
        } else if (r.kind === "workspace-create") {
          // Add a workspace (DB insert via the store) and open it. r.cmd = name,
          // r.cwd = path. The store reloads so the Sidebar reflects it immediately.
          try {
            const ws = await wsStore.create(r.cmd, r.cwd);
            wsStore.open(ws);
          } catch (err) {
            console.error("burrow workspace-create failed:", err);
          }
        } else if (uiStore.spawnMode === "chat") {
          // Sub-agent spawn opens as a chat instead of a terminal tab. Seed the
          // prompt as the input draft (ClaudeChat loads DRAFT_KEY on mount) so the
          // user reviews + sends. ponytail: prefill, not auto-send — dodges ACP
          // session-ready timing; `burrow wait` result-capture stays terminal-only.
          const session = chatsStore.create(props.workspaceId, { agentKind: uiStore.defaultChatAgent });
          const prompt = spawnPrompt(r.cmd);
          if (prompt) localStorage.setItem(`burrow.draft.chat.${session.id}`, prompt);
          openClaudeChat(session.id, undefined, r.cwd || undefined);
        } else {
          const leaf = addTab(r.cmd, { cwd: r.cwd || undefined, resultToken: r.token || undefined, background: true });
          if (r.tmuxWin) {
            invoke("register_tmux_win", { winId: r.tmuxWin, ptyId: leaf.id });
          }
        }
      }
    } catch { /* ignore poll errors */ }
  }, 1000);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  if (spawnPoll) clearInterval(spawnPoll);
  leafUnlisteners.forEach((fns) => fns.forEach((fn) => fn()));
  leafUnlisteners.clear();
  tabsStore.clear(props.workspaceId);
});

function popOutTab(tab: Tab) {
  const leaf =
    focusedLeafId.value !== 0
      ? findLeaf(tab.root, focusedLeafId.value) ?? getFirstLeaf(tab.root)
      : getFirstLeaf(tab.root);
  invoke("open_float_window", {
    ptyId: leaf.id,
    title: leaf.title,
    wsId: props.workspaceId,
  });
}

// Activate the tab containing ptyId and focus that leaf. Called by App.vue
// when main window regains focus from a float bubble.
function focusLeaf(ptyId: number) {
  for (const tab of tabs.value) {
    const leaf = findLeaf(tab.root, ptyId);
    if (leaf) {
      activeTabId.value = tab.id;
      nextTick(() => {
        focusedLeafId.value = ptyId;
        xtermRefs.get(ptyId)?.focus();
      });
      return;
    }
  }
}

// Refit every mounted xterm — called when the pane is re-shown after a mode
// switch, where a hidden (display:none) fit could have settled at 0 size.
function refitAll() {
  xtermRefs.forEach((x) => x?.refit?.());
}

// Un-scramble every mounted xterm: clear the renderer atlas + full redraw.
function repaintAll() {
  xtermRefs.forEach((x) => x?.repaint?.());
}

const timelineCollapsed = ref(false);

// ID of the focused agent leaf — used to drive AgentTimeline display.
const activeAgentLeafId = computed((): number | null => {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return null;
  const leaf = findLeaf(tab.root, focusedLeafId.value) ?? getFirstLeaf(tab.root);
  return leaf.isAgent ? leaf.id : null;
});


defineExpose({ addTab, spawnAgent, adoptPty, openDiffInTab, openFileInTab, insertContext, focusLeaf, openClaudeChat, openBrowserTab, refitAll, repaintAll });
</script>

<style scoped>
.terminal-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--terminal-bg);
  backdrop-filter: var(--blur-terminal, none);
  -webkit-backdrop-filter: var(--blur-terminal, none);
  overflow: hidden;
  min-width: 0;
}

.workspace-surface-switcher {
  height: 33px;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 0 10px;
  background: color-mix(in srgb, var(--bg-panel) 92%, var(--terminal-bg));
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.surface-switch-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: 5px;
  color: var(--text-muted);
  background: transparent;
  font: 600 10.5px/1 var(--font-ui);
  cursor: pointer;
  transition: color .12s, background .12s, border-color .12s;
}
.surface-switch-btn:hover { color: var(--text-secondary); background: var(--bg-hover); }
.surface-switch-btn.active {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--accent) 11%, var(--bg-panel));
  border-color: color-mix(in srgb, var(--accent) 24%, var(--border));
}
.surface-switch-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.surface-switch-rule { width: 1px; height: 13px; margin: 0 5px; background: var(--border); }
.surface-switch-context { color: var(--text-muted); font: 500 10px/1 var(--font-mono); }

/* ── Tab bar ───────────────────────────────────────────────────── */
.terminal-tabs {
  display: flex;
  align-items: center;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  padding: 0 6px;
  flex-shrink: 0;
  overflow-x: auto;
  gap: 1px;
}

.tab {
  display: flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11.5px;
  font-family: var(--font-ui);
  padding: 5px 9px 5px 8px;
  white-space: nowrap;
  transition: color .1s, background .1s;
  max-width: 200px;
  flex-shrink: 0;
  position: relative;
  touch-action: none;
}
.tab:hover {
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--border) 35%, transparent);
}
.tab.active {
  color: var(--text-primary);
  border-bottom-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 7%, transparent);
}

/* ── Add / Claude-chat buttons ─────────────────────────────────── */
.tab-add {
  color: var(--text-muted);
  max-width: none;
  opacity: 0.5;
  padding: 5px 7px;
}
.tab-add:hover { color: var(--text-secondary); opacity: 1; background: var(--bg-hover); }

/* ── Drag feedback ─────────────────────────────────────────────── */
.tab.dragging { opacity: 0.4; }
.tab.drag-over { background: color-mix(in srgb, var(--accent) 12%, transparent); }
.tab.drag-over::before {
  content: "";
  position: absolute;
  left: 0;
  top: 4px;
  bottom: 4px;
  width: 2px;
  border-radius: 2px;
  background: var(--accent);
}
.tab-move-move { transition: transform .22s cubic-bezier(.2, .8, .2, 1); }

/* ── Tab label ─────────────────────────────────────────────────── */
.tab-label {
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 140px;
}

/* ── Icons ─────────────────────────────────────────────────────── */
.tab-agent-icon { color: var(--accent); flex-shrink: 0; }
.tab-term-icon  { color: var(--text-muted); flex-shrink: 0; }
.tab-chat-icon  { color: var(--text-secondary); flex-shrink: 0; }
.tab.active .tab-term-icon { color: var(--text-secondary); }

/* ── Status text ───────────────────────────────────────────────── */
.tab-status-text {
  font-size: 10px;
  color: var(--text-muted);
  opacity: 0.65;
  flex-shrink: 0;
  max-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Flash animation ───────────────────────────────────────────── */
@keyframes tab-flash-anim {
  0%   { color: var(--accent); }
  50%  { color: var(--accent); opacity: 0.4; }
  100% { color: inherit; }
}
.tab-flash { animation: tab-flash-anim 0.6s ease-out; }

/* ── Split-pane count badge ────────────────────────────────────── */
.tab-split-count {
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

/* ── Dirty dot ─────────────────────────────────────────────────── */
.dirty-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--text-muted);
}

/* ── Close / float buttons ─────────────────────────────────────── */
.tab-close,
.tab-float {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  border-radius: 3px;
  flex-shrink: 0;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.1s, background 0.1s;
}
.tab:hover .tab-close,
.tab:hover .tab-float { opacity: 0.45; }
.tab-close:hover { opacity: 1 !important; background: rgba(239, 68, 68, 0.18); color: var(--red); }
.tab-float:hover { opacity: 1 !important; background: rgba(255, 255, 255, 0.09); }

/* Agent turn timeline — shown below the tab/log strips for agent tabs */
.agent-timeline-strip {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border);
  background: var(--bg-panel);
}

.tl-strip-header {
  height: 20px;
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 0 8px;
  font-size: 10px;
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
  border-bottom: 1px solid var(--border);
  transition: background 0.1s;
}
.agent-timeline-strip.collapsed .tl-strip-header {
  border-bottom: none;
}
.tl-strip-header:hover { background: var(--bg-hover); }
.tl-strip-header span { font-family: var(--font-ui); }
.tl-strip-turns {
  margin-left: auto;
  font-family: var(--font-mono) !important;
  font-size: 9px;
}

.tl-strip-body {
  height: 90px;
  overflow-y: auto;
  overflow-x: hidden;
}

.terminal-body {
  flex: 1;
  display: flex;
  overflow: hidden;
  position: relative;
}

.terminal-tab-content {
  flex: 1;
  position: relative;
  overflow: hidden;
  min-width: 0;
  min-height: 0;
  background: var(--border);
}

.pane {
  position: absolute;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--terminal-bg);
}

.pane-divider {
  position: absolute;
  z-index: 5;
  background: transparent;
}
.pane-divider:hover { background: var(--accent); opacity: 0.4; }

.drop-zone {
  position: absolute;
  z-index: 20;
  pointer-events: none;
  transition: background 0.12s, opacity 0.12s;
}
.dz-left   { left: 0;   top: 0; width: 25%; height: 100%; }
.dz-right  { right: 0;  top: 0; width: 25%; height: 100%; }
.dz-top    { left: 0;   top: 0; width: 100%; height: 25%; }
.dz-bottom { left: 0; bottom: 0; width: 100%; height: 25%; }
.drop-zone.dz-active {
  background: color-mix(in srgb, var(--accent) 28%, transparent);
  box-shadow: inset 0 0 0 2px var(--accent);
}

.pane.focused::after {
  content: "";
  position: absolute;
  inset: 0;
  pointer-events: none;
  border: 1px solid var(--accent);
  opacity: 0.35;
  z-index: 1;
}

.confirm-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9000;
}

.confirm-modal {
  width: 360px;
  background: #111111;
  border: 1px solid #2a2a2a;
  border-radius: 12px;
  padding: 20px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6), 0 1px 0 rgba(255, 255, 255, 0.08);
}

.confirm-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.confirm-body {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
  margin-bottom: 18px;
}

.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.confirm-btn {
  font-family: var(--font-ui);
  font-size: 12px;
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  color: var(--text-primary);
  cursor: pointer;
}
.confirm-btn:hover { background: #222222; }

.confirm-btn.danger {
  background: rgba(239, 68, 68, 0.15);
  border-color: rgba(239, 68, 68, 0.4);
  color: #f87171;
}
.confirm-btn.danger:hover { background: rgba(239, 68, 68, 0.25); }
.confirm-kbd {
  margin-left: 6px;
  opacity: 0.6;
  font-size: 11px;
}

.terminal-welcome {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--text-secondary);
  background: var(--terminal-bg);
}
.welcome-icon { color: var(--text-muted); }
.welcome-title { font-size: 14px; font-weight: 500; color: var(--text-primary); }
.welcome-sub { font-size: 12px; color: var(--text-muted); }
.welcome-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  padding: 7px 16px;
  background: var(--accent);
  border: none;
  border-radius: 5px;
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.welcome-btn:hover { background: var(--accent-dim); }

.pane-titlebar {
  display: flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 8px;
  background: #111111;
  border-bottom: 1px solid #1e1e1e;
  flex-shrink: 0;
  font-size: 11px;
  color: var(--text-secondary);
}
.pane-title-icon { color: var(--text-muted); flex-shrink: 0; }
.pane-title-icon.agent { color: var(--accent); }
.pane-title-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
}
.pane-title-close {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  padding: 2px;
  border-radius: 3px;
  flex-shrink: 0;
  opacity: 0.4;
}
.pane-titlebar:hover .pane-title-close { opacity: 0.8; }
.pane-title-close:hover { opacity: 1 !important; color: var(--red); background: rgba(239,68,68,0.15); }

/* Progress bar inside a tab button */
.tab-progress-bar {
  display: inline-flex;
  align-items: center;
  width: 36px;
  height: 3px;
  border-radius: 2px;
  background: rgba(255,255,255,0.1);
  flex-shrink: 0;
  overflow: hidden;
}
.tab-progress-fill {
  height: 100%;
  border-radius: 2px;
  background: var(--accent);
  transition: width 0.3s ease;
  min-width: 2px;
}

/* Log strip below the tab bar */
.log-strip {
  display: flex;
  flex-direction: column;
  gap: 1px;
  background: color-mix(in srgb, var(--bg-panel) 95%, var(--accent) 5%);
  border-bottom: 1px solid var(--border);
  padding: 2px 10px;
  flex-shrink: 0;
  max-height: 80px;
  overflow: hidden;
}
.log-entry {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 10px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.log-level {
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  opacity: 0.6;
  flex-shrink: 0;
}
.log-entry.log-info .log-level  { color: var(--accent); }
.log-entry.log-warn .log-level  { color: var(--yellow); }
.log-entry.log-error .log-level { color: var(--red); }
.log-msg { overflow: hidden; text-overflow: ellipsis; }
.log-fade-enter-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.log-fade-enter-from  { opacity: 0; transform: translateY(-4px); }
.log-fade-leave-active { transition: opacity 0.15s ease; position: absolute; }
.log-fade-leave-to    { opacity: 0; }
</style>
