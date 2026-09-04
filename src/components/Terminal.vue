<template>
  <div class="terminal-pane relative flex min-w-0 flex-1 flex-col overflow-hidden bg-[var(--terminal-bg)] [-webkit-backdrop-filter:var(--blur-terminal,none)] [backdrop-filter:var(--blur-terminal,none)]" @click="focusActive">
    <!-- Log strip: last entries from `burrow log` for the active tab -->
    <TransitionGroup
      v-if="activeTabLogs.length"
      name="log-fade"
      tag="div"
      class="log-strip flex max-h-20 shrink-0 flex-col gap-px overflow-hidden border-b border-border bg-[color-mix(in_srgb,var(--bg-panel)_95%,var(--accent)_5%)] px-2.5 py-0.5"
    >
      <div
        v-for="entry in activeTabLogs"
        :key="entry.ts"
        class="flex items-baseline gap-1.5 overflow-hidden text-ellipsis whitespace-nowrap text-[10px] leading-[1.5] text-secondary-foreground"
      >
        <span
          class="shrink-0 text-[9px] font-semibold uppercase tracking-[0.04em] opacity-60"
          :class="{ 'text-accent': entry.level === 'info', 'text-warning': entry.level === 'warn', 'text-destructive': entry.level === 'error' }"
        >{{ entry.level }}</span>
        <span class="overflow-hidden text-ellipsis">{{ entry.message }}</span>
      </div>
    </TransitionGroup>

    <AgentPlanRail
      v-if="activeAgentLeafId !== null"
      :pty-id="activeAgentLeafId"
    />

    <AgentInspector
      v-if="inspectorAgent"
      :agent="inspectorAgent"
      :open="inspectorOpen"
      class="absolute right-2.5 top-[76px] z-30"
      @focus="focusInspectedAgent"
      @follow-up="openFollowUpChat"
      @stop="stopInspectedAgent"
      @dismiss="inspectorOpen = false"
    />

    <div
      v-if="tabs.length > 0"
      class="terminal-body relative flex flex-1 overflow-hidden"
      :class="{ 'split-workspace gap-px bg-border max-[860px]:flex-col': splitWorkspace }"
    >
      <div
        v-for="tab in tabs"
        :key="tab.id"
        class="terminal-tab-content relative min-h-0 min-w-0 flex-1 overflow-hidden bg-border"
        :class="{
          'flex-[1_1_50%] min-w-[280px] max-[860px]:min-h-[220px]': splitWorkspace,
          'surface-focused shadow-[inset_0_2px_0_var(--accent)]': splitWorkspace && activeTabId === tab.id,
        }"
        v-show="isTabVisible(tab)"
      >
        <div
          v-for="pane in paneLayout(tab)"
          :key="pane.leaf.id"
          class="pane absolute flex flex-col overflow-hidden bg-[var(--terminal-bg)]"
          :class="{ focused: focusedLeafId === pane.leaf.id && isTabSplit(tab) }"
          :style="rectStyle(pane.rect)"
          :data-leaf-id="pane.leaf.id"
          @mousedown.capture="activateLeaf(pane.leaf.id)"
        >
          <div v-if="isTabSplit(tab)" class="pane-titlebar group flex h-[26px] shrink-0 items-center gap-[5px] border-b border-[#1e1e1e] bg-[#111111] px-2 text-[11px] text-secondary-foreground" @mousedown.stop>
            <PhFileCode v-if="pane.leaf.leafType === 'editor'" :size="10" class="shrink-0 text-muted-foreground" />
            <PhGlobe v-else-if="pane.leaf.leafType === 'browser'" :size="10" class="shrink-0 text-muted-foreground" />
            <PhRobot v-else-if="pane.leaf.isAgent" :size="10" class="shrink-0 text-accent" />
            <PhTerminal v-else :size="10" class="shrink-0 text-muted-foreground" />
            <span v-if="pane.leaf.leafType === 'editor' && pane.leaf.dirty" class="dirty-dot h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground" />
            <span
              v-else-if="pane.leaf.status !== 'idle'"
              class="status-dot"
              :class="`status-${pane.leaf.status}`"
              :title="pane.leaf.status === 'error' ? pane.leaf.statusDetail : undefined"
            >{{ pane.leaf.status === 'running' ? spinnerFrame : '' }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px]">{{ pane.leaf.title }}</span>
            <button class="flex shrink-0 items-center rounded-[3px] p-0.5 text-muted-foreground opacity-40 transition-opacity group-hover:opacity-80 hover:!opacity-100 hover:!text-destructive hover:!bg-destructive/[0.15]" @click.stop="closePane(pane.leaf.id)" title="Close pane">
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
          <AgentChat
            v-else-if="pane.leaf.leafType === 'chat' && shouldMountChat(tab, pane.leaf)"
            :key="`chat-${pane.leaf.chatId}`"
            :chat-id="pane.leaf.chatId!"
            :workspace-id="workspaceId"
            :cwd="pane.leaf.cwd ?? cwd"
            :is-watching="isWatching(tab)"
            :initial-prompt="pane.leaf.initialPrompt"
            :initial-images="pane.leaf.initialImages"
            :default-model="pane.leaf.initialModel"
            @prompt-sent="pane.leaf.initialPrompt = undefined"
          />
          <BrowserPane
            v-else-if="pane.leaf.leafType === 'browser'"
            :initial-url="pane.leaf.browserUrl"
          />
          <GitPanel v-else-if="pane.leaf.leafType === 'git'" class="min-w-0 flex-1" />
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
          class="pane-divider absolute z-[5] bg-transparent transition-[background,opacity] duration-150 hover:bg-accent hover:opacity-40"
          :style="dividerStyle(div)"
          @mousedown.stop.prevent="startDividerDrag($event, div)"
        />
      </div>
    </div>
    <div v-else class="terminal-welcome flex flex-1 flex-col items-center justify-center gap-3 bg-[var(--terminal-bg)] text-secondary-foreground">
      <PhTerminalWindow :size="40" weight="thin" class="text-muted-foreground" />
      <p class="text-sm font-medium text-foreground">No terminals open</p>
      <p class="text-xs text-muted-foreground">Launch an agent above or open a new terminal</p>
      <button class="mt-2 flex items-center gap-1.5 rounded-md border-0 bg-accent px-4 py-[7px] text-xs font-semibold text-white hover:bg-[var(--accent-dim)]" @click="addTab()">
        <PhPlus :size="13" /> New Terminal
      </button>
    </div>

    <div
      v-show="bottomPanelFor(activeTabId).open"
      class="bottom-panel-resize-handle"
      @mousedown="startBottomResize"
    />
    <BottomTerminalPanel
      v-for="tab in tabsWithBottomPanel"
      :key="tab.id"
      v-show="tab.id === activeTabId && bottomPanelFor(tab.id).open"
      :state="bottomPanelFor(tab.id)"
      :cwd="props.cwd"
      class="shrink-0"
      :style="{ height: bottomPanelFor(tab.id).height + 'px' }"
      @add="addBottomTerminal(tab.id)"
      @close="(ptyId) => closeBottomTerminal(tab.id, ptyId)"
      @select="(ptyId) => bottomPanelFor(tab.id).activeId = ptyId"
    />

    <div v-if="confirm" class="confirm-overlay fixed inset-0 z-[9000] flex items-center justify-center bg-black/60" @mousedown.self="answerClose(false)">
      <div class="w-[360px] rounded-xl border border-[#2a2a2a] bg-[#111111] p-5 shadow-[0_24px_64px_rgba(0,0,0,0.6),0_1px_0_rgba(255,255,255,0.08)]">
        <div class="mb-2 text-sm font-semibold text-foreground">{{ confirm.reason === 'unsaved' ? 'Unsaved changes' : 'Close terminal' }}</div>
        <div class="mb-[18px] text-xs leading-[1.5] text-secondary-foreground">
          "{{ confirm.name }}" {{ confirm.reason === 'unsaved' ? 'has unsaved changes' : 'has a running process' }}. Close anyway?
        </div>
        <div class="flex justify-end gap-2">
          <button class="rounded-md border border-border bg-hover px-3.5 py-1.5 font-sans text-xs text-foreground hover:bg-[#222222]" @click="answerClose(false)">Cancel</button>
          <button class="rounded-md border border-destructive/40 bg-destructive/[0.15] px-3.5 py-1.5 font-sans text-xs text-[#f87171] hover:bg-destructive/25" @click="answerClose(true)">Close <span class="ml-1.5 text-[11px] opacity-60">⌘↵</span></button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { createActor, type Actor } from "xstate";
import { agentStatusMachine, isBusyStatus } from "@/machines/agentStatus";
import { PhRobot, PhTerminal, PhTerminalWindow, PhX, PhPlus, PhFileCode, PhGlobe } from "@phosphor-icons/vue";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useProvidersStore } from "@/stores/providers";
import { providerIdForCommand } from "@/lib/providers";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import XTerm from "./XTerm.vue";
import DiffTab from "./DiffTab.vue";
import CodeEditor from "./CodeEditor.vue";
import AgentChat from "./AgentChat.vue";
import BrowserPane from "./BrowserPane.vue";
import BottomTerminalPanel from "./BottomTerminalPanel.vue";
import GitPanel from "./GitPanel.vue";
import { type Leaf, type TreeNode, type SplitNode } from "./TerminalSplitView.vue";
import { nextPtyId, initPtyCounter } from "@/lib/ptyId";
import { spinnerFrame } from "@/lib/spinner";
import { dropChatSession } from "@/lib/chatSession";
import { router } from "@/router";
import { configReady } from "@/lib/config";
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
import AgentPlanRail from "@/components/AgentPlanRail.vue";
import AgentInspector, { type AgentInspectorAgent } from "@/components/AgentInspector.vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore } from "@/stores/ui";
import { useKeybindingsStore } from "@/stores/keybindings";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useNotificationsStore } from "@/stores/notifications";
import { useGitStore } from "@/stores/git";
import { isTabSettled, settledTabKeys } from "@/lib/settledTabs";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";

const props = defineProps<{ cwd: string; workspaceId: number }>();
const wsStore = useWorkspaceStore();
const uiStore = useUIStore();
const chatsStore = useClaudeChatsStore();
const providersStore = useProvidersStore();
const tabsStore = useTerminalTabsStore();
const keys = useKeybindingsStore();
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
const splitWorkspace = ref(false);
const inspectorOpen = ref(false);
const lastChatTabId = ref(0);
const lastTerminalTabId = ref(0);

// ── Bottom terminal panel: one per thread (tab), ⌘J toggles the active one ──
// Kept-mounted per tab (like the RightPanel scratch terminal) so switching
// threads and back doesn't drop output or respawn the shell; killed only when
// the owning thread itself closes.
interface BottomPanelState { open: boolean; ptyIds: number[]; activeId: number | null; height: number }
const bottomPanels = reactive<Record<number, BottomPanelState>>({});
function bottomPanelFor(tabId: number): BottomPanelState {
  return (bottomPanels[tabId] ??= { open: false, ptyIds: [], activeId: null, height: 220 });
}
const tabsWithBottomPanel = computed(() => tabs.value.filter((t) => bottomPanels[t.id]));

function addBottomTerminal(tabId: number) {
  const s = bottomPanelFor(tabId);
  const id = nextPtyId();
  s.ptyIds.push(id);
  s.activeId = id;
}

async function closeBottomTerminal(tabId: number, ptyId: number) {
  const s = bottomPanelFor(tabId);
  await invoke("kill_pty", { id: ptyId }).catch(() => {});
  s.ptyIds = s.ptyIds.filter((id) => id !== ptyId);
  if (s.activeId === ptyId) s.activeId = s.ptyIds[s.ptyIds.length - 1] ?? null;
}

function toggleBottomPanel() {
  const s = bottomPanelFor(activeTabId.value);
  s.open = !s.open;
  if (s.open && s.ptyIds.length === 0) addBottomTerminal(activeTabId.value);
}

function startBottomResize(e: MouseEvent) {
  const s = bottomPanelFor(activeTabId.value);
  const startY = e.clientY;
  const startHeight = s.height;
  function onMove(ev: MouseEvent) {
    s.height = Math.min(window.innerHeight * 0.8, Math.max(120, startHeight - (ev.clientY - startY)));
  }
  function onUp() {
    window.removeEventListener("mousemove", onMove);
    window.removeEventListener("mouseup", onUp);
  }
  window.addEventListener("mousemove", onMove);
  window.addEventListener("mouseup", onUp);
}

async function killBottomPanel(tabId: number) {
  const s = bottomPanels[tabId];
  if (!s) return;
  await Promise.all(s.ptyIds.map((id) => invoke("kill_pty", { id }).catch(() => {})));
  delete bottomPanels[tabId];
}
const splitChatTab = computed(() =>
  tabs.value.find((tab) => tab.id === lastChatTabId.value && tabIsChat(tab))
  ?? tabs.value.find(tabIsChat),
);
const splitTerminalTab = computed(() =>
  tabs.value.find((tab) => tab.id === lastTerminalTabId.value && !tabIsChat(tab))
  ?? tabs.value.find((tab) => !tabIsChat(tab)),
);
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

function tabIsChat(tab: Tab): boolean {
  return tab.root.type === "leaf" && tab.root.leafType === "chat";
}

function isTabVisible(tab: Tab): boolean {
  if (!splitWorkspace.value) return activeTabId.value === tab.id;
  return tab.id === splitChatTab.value?.id || tab.id === splitTerminalTab.value?.id;
}

// A chat leaf is MOUNTED only while it is genuinely on screen — not merely
// v-show'd behind the welcome composer, another workspace or another tab. That
// is the whole point of fáze 3 (docs/plans/003-view-state-routes.md): "is the
// user looking at this?" stops being a predicate anyone can forget to call and
// becomes whether the component exists at all. Its stream keeps running
// regardless — lib/chatSession.ts owns that, not the component.
//
// Terminals deliberately do NOT get this treatment: reattaching a PTY replays a
// ring buffer into a re-fitted xterm and corrupts the scrollback (fáze 5).
function isChatVisible(tab: Tab): boolean {
  return isTabVisible(tab) && uiStore.viewingTabs && wsStore.active?.id === props.workspaceId;
}

// ...with one exception: a chat spawned with a prompt has to start its agent
// even if it landed in a workspace the user is not looking at. Without this a
// background `burrow spawn` would sit unsent until someone opened the tab.
// Such a chat mounts unwatched, so AgentChat gates "mark seen" on isWatching
// rather than on mount alone.
function shouldMountChat(tab: Tab, leaf: Leaf): boolean {
  return isChatVisible(tab) || leaf.initialPrompt != null;
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

function makeLeaf(initialCmd?: string, extra?: { cwd?: string; resultToken?: string; id?: number }): Leaf {
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
  };
}

// ── events from split tree ──────────────────────────────────────────────────

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

// True when the user is actively looking at this tab: the terminal host is
// showing tabs (not the welcome composer — `viewingTabs`), its workspace is the
// visible one, this tab is the active tab, and the window has OS focus.
function isWatching(tab: Tab): boolean {
  return (
    uiStore.viewingTabs &&
    wsStore.active?.id === props.workspaceId &&
    activeTabId.value === tab.id &&
    document.hasFocus()
  );
}

// Mark every finished TERMINAL leaf in a tab as seen (user opened/returned to
// it). Terminal leaves stay mounted for their whole life, so nothing about them
// says "on screen" — this is where that gets decided for them.
function markTabSeen(tab: Tab) {
  for (const leaf of getAllLeaves(tab.root)) {
    // Chats are not marked here any more: a chat is mounted only when it is on
    // screen, so it marks ITSELF seen on mount. Marking from out here was the
    // bug — this code had to guess at visibility, and a tab behind the welcome
    // composer looked watched.
    if (leaf.leafType === "chat") continue;
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
    // Snapshot the worktree before the agent touches it, so this turn is
    // revertable from the History panel. No-op outside a git repo.
    invoke("create_checkpoint", {
      cwd: leaf.cwd ?? props.cwd,
      ptyId: leaf.id,
      label: leaf.title || "Agent turn",
    }).catch(() => {}); // best-effort: never block a turn on the snapshot
  }
  historyStore.addEvent(leaf.id, s);
  const actor = leafActors.get(id);
  if (!actor) return;

  // A terminal thread can be attached after its first hook already fired. The
  // Wails hook server replays that latest state, but the status machine begins
  // at idle and normally only accepts START there. Rebuild the minimal valid
  // transition path so replayed waiting/done/error states reach the sidebar
  // mirror just like their live counterparts.
  if (leaf.status === "idle") {
    if (s === "waiting") actor.send({ type: "START" });
    else if (s === "done") actor.send({ type: "START" });
    else if (s === "error") actor.send({ type: "START" });
  }
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

const activeTabLogs = computed(() => {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return [];
  return (tabLogs.value[tab.id] ?? []).slice(-5);
});

// Terminal stays the orchestration surface; the inspector is deliberately
// presentational and receives a stable snapshot through typed props.
const inspectorAgent = computed<AgentInspectorAgent | null>(() => {
  const tab = tabs.value.find((candidate) => candidate.id === activeTabId.value);
  if (!tab) return null;
  const leaf = findLeaf(tab.root, focusedLeafId.value) ?? getAllLeaves(tab.root)[0];
  const logs = activeTabLogs.value;
  const latest = logs[logs.length - 1];
  return {
    title: tabTitle(tab),
    status: tabStatus(tab),
    cwd: leaf?.cwd ?? props.cwd,
    recentActivity: leaf?.statusText ?? latest?.message ?? null,
    recentActivityAt: latest ? new Date(latest.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : null,
    terminal: {
      id: leaf?.id ?? tab.id,
      label: leaf?.title,
      ptyId: leaf?.id,
      workspaceId: props.workspaceId,
    },
  };
});

function focusInspectedAgent() {
  inspectorOpen.value = false;
  focusActive();
}

function openFollowUpChat() {
  inspectorOpen.value = false;
  openClaudeChat();
}

function stopInspectedAgent() {
  inspectorOpen.value = false;
  closeTab(activeTabId.value);
}

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
  // The open tab is part of the address, so selecting one is a navigation.
  // `replace`, not `push`: clicking through tabs should not fill the back stack.
  if (wsStore.active?.id === props.workspaceId && uiStore.viewingTabs) {
    void router.replace(`/ws/${props.workspaceId}/tab/${id}`);
  }
  // User is now looking at this tab — clear any done/review badge.
  markTabSeen(tab);
  const leaf = getFirstLeaf(tab.root);
  focusedLeafId.value = leaf.id;
  nextTick(() => xtermRefs.get(leaf.id)?.focus());
}

function openClaudeChat(chatId?: number, agentId?: string, cwd?: string, initialPrompt?: string, initialImages?: string[], model?: string) {
  let session: import("@/stores/claudeChats").ClaudeSession;
  if (chatId != null) {
    session = chatsStore.sessions.find((s) => s.id === chatId) ?? chatsStore.create(props.workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent });
  } else {
    session = chatsStore.create(props.workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent });
  }
  // Reopening an archived chat (e.g. from the Sidebar's Archived shelf) always
  // un-archives it — a chat with an open tab is never archived.
  if (session.archivedAt) chatsStore.unarchive(session.id);
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
    initialModel: chatId == null ? model : undefined,
  };
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activateTab(tab.id);
  // ClaudeChat sends this itself once its runtime is up (a chat leaf has no
  // xtermRefs entry, and its CLI/ACP session isn't ready on the next tick).
  if (initialPrompt) {
    leaf.initialPrompt = initialPrompt;
    leaf.initialImages = initialImages;
  }
}

function activateLeaf(id: number) {
  const tab = tabs.value.find((candidate) => findLeaf(candidate.root, id));
  if (!tab) return;
  activeTabId.value = tab.id;
  focusedLeafId.value = id;
  markTabSeen(tab);
  nextTick(() => xtermRefs.get(id)?.focus());
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

/** Full git manager as a tab (leafType "git") — same slot as browser/editor tabs. */
function openGitTab() {
  const existing = tabs.value.find((t) => t.root.type === "leaf" && t.root.leafType === "git");
  if (existing) { activateTab(existing.id); return; }
  const leaf: Leaf = {
    type: "leaf",
    id: nextPtyId(),
    title: "Git",
    defaultTitle: "Git",
    isAgent: false,
    busy: false,
    status: "idle",
    leafType: "git",
  };
  const tab: Tab = { id: leaf.id, root: leaf };
  tabs.value.push(tab);
  activateTab(tab.id);
}

function addTab(initialCmd?: string, extra?: { cwd?: string; resultToken?: string; background?: boolean }): Leaf {
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
function openFileInTab(path: string, name: string, line?: number) {
  for (const tab of tabs.value) {
    const existing = getAllLeaves(tab.root).find(
      (l) => l.leafType === "editor" && l.filePath === path,
    );
    if (existing) {
      activeTabId.value = tab.id;
      markTabSeen(tab);
      focusedLeafId.value = existing.id;
      nextTick(() => {
        if (line) xtermRefs.get(existing.id)?.revealLine?.(line);
        xtermRefs.get(existing.id)?.focus();
      });
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
    fileLine: line,
    dirty: false,
  };
  const tab: Tab = { id, root: leaf };
  tabs.value.push(tab);
  activeTabId.value = id;
  focusedLeafId.value = id;
  nextTick(() => xtermRefs.get(id)?.focus());
}

function makeChatLeaf(agentId?: string): Leaf {
  const session = chatsStore.create(props.workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent });
  const id = nextPtyId();
  return { type: "leaf", id, title: session.title, defaultTitle: session.title, isAgent: false, busy: false, status: "idle", leafType: "chat", chatId: session.id, cwd: props.cwd };
}

function splitFocused(kind: "terminal" | "chat", direction: "h" | "v") {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return;
  const newLeaf = kind === "chat" ? makeChatLeaf() : makeLeaf();
  tab.root = insertSplit(tab.root, focusedLeafId.value, direction, newLeaf);
  focusedLeafId.value = newLeaf.id;
  if (kind === "terminal") registerLeafListeners(newLeaf.id);
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
  await killBottomPanel(tabId);

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

// Explicit teardown of a chat leaf's backend adapter/CLI. Called ONLY on user
// close (closeTab/closePane) — ClaudeChat.onBeforeUnmount deliberately no longer
// stops the proc, so a background remount can't kill a live agent. Transport is
// per-session; a wrong-map stop is a harmless no-op, but resolve it to be clean.
// Closing a thread archives it (kills the process, keeps the row): restore
// filters archived sessions out of `sessionsForWs`, so it won't resurrect as an
// open tab on the next launch, but it stays reachable from the Archived shelf.
function stopChatSession(leaf: Leaf) {
  if (leaf.chatId == null) return;
  // The chat is going away for good, so the session's listeners have nothing
  // left to feed — this is the one place they are torn down (lib/chatSession.ts
  // otherwise keeps them for the running turn behind an unmounted view).
  dropChatSession(leaf.chatId);
  chatsStore.archive(leaf.chatId);
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
  // ⌃1-9 tab switch — a range, so not part of the rebindable registry.
  if (e.ctrlKey && !e.metaKey && !e.altKey && /^[1-9]$/.test(e.key)) {
    e.preventDefault();
    const idx = parseInt(e.key) - 1;
    const wsTabs = tabsStore.tabsByWs[props.workspaceId] ?? [];
    if (wsTabs[idx]) activateTab(wsTabs[idx].id);
    return;
  }
  // Jump to first unread (review) tab in THIS workspace; App.vue's handler
  // covers the cross-workspace case when there's none here.
  if (keys.matches(e, "unread")) {
    const reviewTab = tabs.value.find((t) => tabStatus(t) === "review");
    if (reviewTab) {
      e.preventDefault();
      activateTab(reviewTab.id);
    }
    return;
  }
  const actions: Record<string, () => void> = {
    newTab: () => addTab(),
    closePane: () => closePane(focusedLeafId.value),
    splitH: () => splitFocused("terminal", "h"),
    splitV: () => splitFocused("terminal", "v"),
    bottomPanel: () => toggleBottomPanel(),
  };
  for (const cmd of keys.inScope("terminal")) {
    if (actions[cmd.id] && keys.matches(e, cmd.id)) {
      e.preventDefault();
      actions[cmd.id]();
      return;
    }
  }
}

// ── persistence ─────────────────────────────────────────────────────────────

function allLeaves(): Leaf[] {
  return tabs.value.flatMap((t) => getAllLeaves(t.root));
}

// Flipped once onMounted has finished reading the saved tabs. Until then any
// tab mutation (e.g. a chat request that arrives mid-mount) would persist a
// list that doesn't include the not-yet-restored rows, wiping them.
let restored = false;

function persist() {
  if (!restored) return;
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
}

// Include title, defaultTitle, and sessionId so any of those changes triggers a save.
watch(
  () => allLeaves().map((l) => `${l.id}|${l.title}|${l.defaultTitle}|${l.sessionId ?? ""}`).join(","),
  persist,
);

// ── sidebar mirror ──────────────────────────────────────────────────────────
// Push tab summaries to the shared store so the Sidebar can list terminals
// nested under this workspace, and react to clicks coming back from it.

function chatSessionOf(t: Tab) {
  return tabIsChat(t) ? chatsStore.sessions.find((s) => s.id === (t.root as Leaf).chatId) : undefined;
}

// Branch shown in the Sidebar is a snapshot taken the first time each tab is
// synced, not the workspace's live branch — otherwise every tab silently
// re-labels itself as the repo moves on to a new branch. Cache is per-tab-id
// and never overwritten once set.
const tabBranchSnapshot = new Map<number, string>();
function branchSnapshotFor(tabId: number): string | undefined {
  if (!tabBranchSnapshot.has(tabId)) {
    const ws = wsStore.workspaces.find((w) => w.id === props.workspaceId);
    const live = ws?.worktree_branch || gitStore.branchByWs[props.workspaceId];
    if (live) tabBranchSnapshot.set(tabId, live);
  }
  return tabBranchSnapshot.get(tabId);
}

/** Icon key for the agent behind a tab: the chat's provider, else the provider
 *  matched from a launched agent's command. Undefined = plain terminal. */
function tabAgentIcon(t: Tab): string | undefined {
  const chat = chatSessionOf(t);
  if (chat) return providersStore.byId(chat.agentKind)?.icon;
  if (!tabIsAgent(t)) return undefined;
  const cmd = getAllLeaves(t.root)[0]?.initialCmd ?? "";
  return cmd ? providersStore.byId(providerIdForCommand(cmd))?.icon : undefined;
}

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
      sessionId: getAllLeaves(t.root)[0]?.sessionId,
      chatId: tabIsChat(t) ? (t.root as Leaf).chatId : undefined,
      settled: tabIsChat(t)
        ? chatsStore.isSettled(chatsStore.sessions.find((s) => s.id === (t.root as Leaf).chatId))
        : isTabSettled(props.workspaceId, t.id),
      agentIcon: tabAgentIcon(t),
      model: chatSessionOf(t)?.model ?? getAllLeaves(t.root)[0]?.model,
      branch: branchSnapshotFor(t.id),
    })),
  );
  tabsStore.setActive(props.workspaceId, activeTabId.value);
}

watch([tabs, activeTabId, focusedLeafId], syncStore, { deep: true });
// Settle is toggled outside this component's own reactive tree (localStorage-
// backed ref for terminal/agent tabs, settledOverride for chats) — neither
// mutates `tabs`, so the sidebar mirror needs its own trigger or it only
// updates on the next unrelated tab change.
watch(
  () => [settledTabKeys.value.join(","), chatsStore.sessions.map((s) => `${s.id}:${s.settledOverride ?? ""}`).join(",")],
  syncStore,
);
// A tab can be created before the 60s git sweep has ever filled branchByWs
// for this workspace; catch it up once that first value lands, so it still
// gets a real snapshot instead of permanently missing one.
watch(() => gitStore.branchByWs[props.workspaceId], syncStore);

watch(activeTabId, (id) => {
  const tab = tabs.value.find((candidate) => candidate.id === id);
  if (!tab) return;
  if (tabIsChat(tab)) lastChatTabId.value = id;
  else lastTerminalTabId.value = id;
});

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

// Seeing the active tab again clears any "review" badge it earned while hidden.
// Both triggers route through `seeActiveTab`, which re-checks the full view
// state — switching workspace, or coming back to the tabs from the dashboard or
// the welcome composer. Gating on `uiStore.viewingTabs` (not just the mode) is
// what keeps a tab behind the composer from being marked seen unseen.
function seeActiveTab() {
  if (!uiStore.viewingTabs || wsStore.active?.id !== props.workspaceId) return;
  if (!document.hasFocus()) return;
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (tab) markTabSeen(tab);
}

watch(() => wsStore.active?.id, seeActiveTab);
watch(() => uiStore.viewingTabs, seeActiveTab);

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
  else if (req.action === "add") {
    const leaf = addTab(req.cmd || undefined, {
      cwd: req.cwd || undefined,
      resultToken: req.resultToken || undefined,
      background: req.background,
    });
    // Report the id back so a control-API spawn can return it to the caller.
    tabsStore.fulfillAdd(req.nonce, leaf.id);
  }
  else if (req.action === "close" && req.tabId != null) closeTab(req.tabId);
  else if (req.action === "reorder" && req.fromIdx != null && req.toIdx != null) {
    reorderTabs(req.fromIdx, req.toIdx);
  }
  else if (req.action === "openGit") openGitTab();
  else if (req.action === "openChat") openClaudeChat(req.chatId, req.agentId, undefined, req.initialPrompt, req.initialImages, req.model);
  else if (req.action === "rename" && req.tabId != null && req.title != null) {
    const tab = tabs.value.find((t) => t.id === req.tabId);
    if (tab) getAllLeaves(tab.root).forEach((l) => { l.title = req.title!; });
  }
}

watch(() => tabsStore.request, applyTabRequest);

// Coming back to the window counts as seeing the active tab. Without this a turn
// that finished while the app was in the background left its "review" badge up
// until the user clicked away and back — the ws/mode watchers below only fire on
// a switch, and returning to an already-active tab is neither.
function onWindowFocus() {
  seeActiveTab();
}

onMounted(async () => {
  window.addEventListener("keydown", onKeydown);
  window.addEventListener("focus", onWindowFocus);

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
  // Exclude the legacy offset id-space (>= 1_000_000) left behind by the removed
  // Mission Control feature: an old daemon session in that range would otherwise
  // shove fresh tab ids up into it. Anything below MC_RANGE_START is a normal tab.
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
    if (!activeTabId.value) activateTab(tabs.value[0].id);
  }

  // Restore chat tabs saved from the previous session. The sessions themselves
  // arrive behind configReady — reading before that resolves finds none of them
  // and silently drops every chat thread.
  await configReady;
  // The sessions ARE the threads — no separate "which tabs were open" list to
  // fall out of sync (a stale empty one used to hide every thread on restart).
  // Skip the hidden Manager session and never-used blanks left by older builds.
  for (const s of chatsStore.sessionsForWs(props.workspaceId)) {
    if (s.control) continue;
    if (!s.claudeSessionId && !s.messageCount) continue;
    openClaudeChat(s.id);
  }

  restored = true;
  persist(); // the restore may have renumbered dead PTYs; save the ids we settled on
  syncStore();
  // Replay a request that landed while this Terminal was mounting/restoring —
  // e.g. the sidebar clicking a tab of a workspace that wasn't open yet.
  applyTabRequest(tabsStore.request);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("focus", onWindowFocus);
  leafUnlisteners.forEach((fns) => fns.forEach((fn) => fn()));
  leafUnlisteners.clear();
  tabsStore.clear(props.workspaceId);
});

// Activate the tab containing ptyId and focus that leaf.
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

// ID of the focused agent leaf — used to drive agent-specific controls.
const activeAgentLeafId = computed((): number | null => {
  const tab = tabs.value.find((t) => t.id === activeTabId.value);
  if (!tab) return null;
  const leaf = findLeaf(tab.root, focusedLeafId.value) ?? getFirstLeaf(tab.root);
  return leaf.isAgent ? leaf.id : null;
});


function splitPane(direction: "h" | "v" = "h") {
  splitFocused("terminal", direction);
}

defineExpose({ addTab, splitPane, spawnAgent, adoptPty, openDiffInTab, openFileInTab, insertContext, focusLeaf, openClaudeChat, openBrowserTab, openGitTab, refitAll, repaintAll });
</script>

<style scoped>
/* ── Tab drag feedback: pseudo-element indicator, kept as plain CSS ──────── */
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

/* ── TransitionGroup FLIP move + fade phase classes ───────────────────── */
.tab-move-move { transition: transform .22s cubic-bezier(.2, .8, .2, 1); }
.log-fade-enter-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.log-fade-enter-from  { opacity: 0; transform: translateY(-4px); }
.log-fade-leave-active { transition: opacity 0.15s ease; position: absolute; }
.log-fade-leave-to    { opacity: 0; }

/* ── Tab-label flash keyframe (flags a leaf that just wrote output) ──────── */
@keyframes tab-flash-anim {
  0%   { color: var(--accent); }
  50%  { color: var(--accent); opacity: 0.4; }
  100% { color: inherit; }
}
.tab-flash { animation: tab-flash-anim 0.6s ease-out; }

.bottom-panel-resize-handle {
  height: 4px;
  cursor: row-resize;
  flex-shrink: 0;
  background: transparent;
  transition: background 0.15s;
}
.bottom-panel-resize-handle:hover,
.bottom-panel-resize-handle:active {
  background: var(--accent);
  opacity: 0.4;
}

/* ── Focused-pane ring: pseudo-element overlay so it never affects layout ── */
.pane.focused::after {
  content: "";
  position: absolute;
  inset: 0;
  pointer-events: none;
  border: 1px solid var(--accent);
  opacity: 0.35;
  z-index: 1;
}
</style>
