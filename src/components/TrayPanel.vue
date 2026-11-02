<template>
  <div
    ref="panel"
    class="tray-panel flex flex-col overflow-hidden rounded-xl border border-border text-foreground"
    :class="fill ? 'h-full' : 'max-h-full'"
  >
    <!-- Header. Pinned, it is also the drag handle (buttons opt out). -->
    <div class="flex shrink-0 items-center gap-1.5 px-2 pb-2 pt-2.5" :style="pinned ? { '--wails-draggable': 'drag' } : undefined">
      <button v-if="view.kind !== 'list'" type="button" class="tray-icon-btn" title="Back" @click="view = { kind: 'list' }">
        <PhCaretLeft :size="14" />
      </button>
      <div class="flex min-w-0 flex-1 items-baseline gap-2" :class="view.kind === 'list' && 'pl-1.5'">
        <span class="truncate text-[13px] font-semibold">{{ headerTitle }}</span>
        <span v-if="view.kind === 'list'" class="truncate text-[11px] text-muted-foreground">{{ summary }}</span>
      </div>
      <button v-if="view.kind === 'chat'" type="button" class="tray-icon-btn" title="Open in Burrow" @click="open(chatRoute(view.chatId))">
        <PhArrowSquareOut :size="14" />
      </button>
      <button type="button" class="tray-icon-btn" :class="pinned && 'text-accent'" :title="pinned ? 'Unpin' : 'Pin — keep on top, movable, resizable'" @click="togglePin">
        <PhPushPin :size="14" :weight="pinned ? 'fill' : 'regular'" />
      </button>
    </div>
    <div class="h-px shrink-0 bg-border" />

    <!-- Thread list -->
    <template v-if="view.kind === 'list'">
      <div ref="list" class="min-h-0 overflow-y-auto px-1.5 pb-1.5" :class="fill ? 'flex-1' : 'shrink'">
        <p v-if="!rows.length" class="px-2 py-8 text-center text-[12px] text-muted-foreground">No active threads.</p>
        <template v-for="group in groups" :key="group.label">
          <template v-if="group.rows.length">
            <div class="px-2 pb-1 pt-2.5 text-[11px] font-medium text-muted-foreground">{{ group.label }}</div>
            <div v-for="row in group.rows" :key="row.key" class="group relative">
              <button
                type="button"
                class="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-hover"
                :title="row.title"
                @click="openRow(row)"
              >
                <span class="relative flex size-4 shrink-0 items-center justify-center">
                  <component :is="row.kind === 'chat' ? PhChatCircle : PhTerminalWindow" :size="15" class="text-muted-foreground" />
                  <span v-if="row.status !== 'idle'" class="absolute -right-0.5 -top-0.5 size-[7px] rounded-full ring-2 ring-[var(--sidebar-bg)]" :class="[dotClass(row.status), row.status === 'running' && 'animate-pulse']" />
                </span>
                <span class="flex min-w-0 flex-1 flex-col">
                  <span class="truncate text-[13px]" :class="row.status === 'idle' ? 'text-secondary-foreground' : 'text-foreground'">{{ row.title }}</span>
                  <span class="truncate text-[11px] text-muted-foreground" :title="row.recap || undefined">{{ row.workspace }}<template v-if="row.detail || row.recap"> · {{ row.detail || row.recap }}</template></span>
                </span>
                <span class="shrink-0 text-[11px]" :class="[textClass(row.status), row.kind === 'chat' && 'group-hover:invisible']">{{ rightLabel(row) }}</span>
              </button>
              <button
                v-if="row.kind === 'chat'"
                type="button"
                class="tray-icon-btn invisible absolute right-1.5 top-2 group-hover:visible"
                :title="row.status === 'running' ? 'Queue a message for when it is free' : 'Reply'"
                @click.stop="toggleReply(row)"
              >
                <PhArrowBendUpLeft :size="14" />
              </button>
              <!-- Quick reply: queued in Go (chat_queue_add), delivered once the turn settles. -->
              <div v-if="replyTo === row.id" class="mx-2 mb-1.5 ml-[34px] flex items-center gap-1.5">
                <input
                  ref="replyEl"
                  v-model="replyText"
                  class="min-w-0 flex-1 rounded-md border border-border bg-transparent px-2 py-1 text-[12px] text-foreground outline-none placeholder:text-muted-foreground focus:border-accent"
                  :placeholder="row.status === 'running' ? 'Queue a follow-up…' : 'Reply…'"
                  @keydown.enter.prevent="sendReply(row)"
                  @keydown.esc.stop.prevent="replyTo = null"
                />
              </div>
              <!-- A Claude tab's tool permission, answerable right here (ptypermissions.go). -->
              <div v-for="req in permsFor(row)" :key="req.id" class="mx-2 mb-1.5 ml-[34px] rounded-lg border border-border px-2.5 py-2">
                <div class="truncate font-mono text-[11px] text-secondary-foreground" :title="permTarget(req)">{{ req.tool_name }}<template v-if="permTarget(req)">: {{ permTarget(req) }}</template></div>
                <div class="mt-1.5 flex gap-1.5">
                  <button type="button" class="tray-chip bg-accent text-accent-foreground hover:opacity-90" @click="answer(req, 'allow')">Allow</button>
                  <button type="button" class="tray-chip hover:bg-hover hover:text-foreground" @click="answer(req, 'always')">Always</button>
                  <button type="button" class="tray-chip hover:bg-hover hover:text-destructive" @click="answer(req, 'deny')">Deny</button>
                </div>
              </div>
            </div>
          </template>
        </template>
      </div>
      <div class="h-px shrink-0 bg-border" />
      <div class="flex shrink-0 items-center justify-between px-1.5 py-1.5">
        <button type="button" class="flex items-center gap-1.5 rounded-lg px-2 py-1 text-[12px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground" @click="startCompose">
          <PhPlus :size="13" /> New thread
        </button>
        <button type="button" class="rounded-lg px-2 py-1 text-[12px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground" @click="open('')">Open Burrow</button>
      </div>
    </template>

    <!-- A thread, live, in the panel. Permissions, questions and the composer are AgentChat's own. -->
    <div v-else-if="view.kind === 'chat'" class="tray-chat relative flex min-h-0 flex-1 flex-col">
      <AgentChat
        v-if="wsReady && chatWs(view.chatId)"
        :key="view.chatId"
        :chat-id="view.chatId"
        :workspace-id="chatWs(view.chatId)!.id"
        :cwd="chatWs(view.chatId)!.path"
        :is-watching="true"
      />
    </div>

    <!-- New thread -->
    <div v-else class="flex min-h-0 flex-1 flex-col gap-2 p-3">
      <Select v-model="composeWs" :options="wsOptions" placeholder="Project" class="w-full" aria-label="Project" />
      <textarea
        ref="composeEl"
        v-model="composeText"
        class="min-h-0 flex-1 resize-none rounded-lg border border-border bg-transparent px-2.5 py-2 text-[13px] text-foreground outline-none placeholder:text-muted-foreground focus:border-accent"
        placeholder="What should the agent do?"
        @keydown.enter.meta.prevent="sendCompose"
      />
      <div class="flex items-center justify-between gap-2">
        <span class="truncate text-[11px] text-destructive">{{ composeError }}</span>
        <button type="button" class="tray-chip shrink-0 bg-accent px-3 py-1 text-[12px] text-accent-foreground hover:opacity-90 disabled:opacity-50" :disabled="!composeText.trim() || !composeWs || composing" @click="sendCompose">
          {{ composing ? "Starting…" : "Start ⌘↵" }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// The tray panel (desktop.go's "tray" window). Unpinned it is a menu-bar
// popover; pinned (TraySetPinned) it stays on top, moves and resizes.
//
// List: the threads the Sidebar lists as active (not settled, archived,
// sub-agents or side chats) plus agent tabs, ordered by what needs the user.
// Status comes from shell_snapshot's phases + this device's receipts.
// A chat row opens the thread right here (AgentChat — secondary windows don't
// notify, chatHost.ts), a tab row in Burrow; a tab's permission is answered inline.
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from "vue";
import { listen } from "@tauri-apps/api/event";
import { invoke } from "@tauri-apps/api/core";
import { Window } from "@wailsio/runtime";
import { PhArrowBendUpLeft, PhArrowSquareOut, PhCaretLeft, PhChatCircle, PhPlus, PhPushPin, PhTerminalWindow } from "@phosphor-icons/vue";
import { TrayOpen, TraySetPinned } from "@/lib/wailsCompat/bindings/burrow/desktopservice";
import { displayStatus, type Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import type { TermStatus } from "@/lib/terminalStatus";
import { settledFor, useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { Select } from "@/components/ui/select";
import { useUIStore } from "@/stores/ui";
import { smartTitle } from "@/lib/chatTitle";

const AgentChat = defineAsyncComponent(() => import("@/components/AgentChat.vue"));

interface Row {
  key: string;
  kind: "chat" | "tab";
  id: number;
  title: string;
  workspace: string;
  detail: string;
  status: TermStatus;
  route: string;
  at: number;
  recap?: string;
}
interface PermRequest {
  id: string;
  pty_id: string;
  tool_name: string;
  tool_input: Record<string, unknown> | null;
}
type View = { kind: "list" } | { kind: "chat"; chatId: number } | { kind: "compose" };

const chats = useClaudeChatsStore();
const workspaces = useWorkspaceStore();
const ui = useUIStore();
const snap = ref<ShellSnapshotData | null>(null);
const perms = ref<PermRequest[]>([]);
const now = ref(Date.now());
const view = ref<View>({ kind: "list" });
const pinned = ref(false);
const wsReady = ref(false);

/** The panel fills the window (pinned, or showing a chat / composer); else it hugs the list. */
const fill = computed(() => pinned.value || view.value.kind !== "list");

function seenAt(): Record<string, number> {
  try {
    return JSON.parse(localStorage.getItem("burrow.seenAt") ?? "{}") as Record<string, number>;
  } catch {
    return {};
  }
}

const chatRoute = (id: number) => {
  const c = chats.sessions.find((s) => s.id === id);
  return c ? `/ws/${c.workspaceId}?chat=${id}` : "";
};
const chatWs = (id: number) => {
  const c = chats.sessions.find((s) => s.id === id);
  return c ? workspaces.workspaces.find((w) => w.id === c.workspaceId) : undefined;
};

const rows = computed<Row[]>(() => {
  const s = snap.value;
  if (!s) return [];
  const seen = seenAt();
  const wsName = new Map(s.workspaces.map((w) => [w.id, w.name]));
  const out: Row[] = [];
  for (const c of chats.sessions) {
    if (c.archivedAt || c.parentChatId || c.kind === "side") continue;
    const phase = s.phases[`chat:${c.id}`];
    const status = displayStatus(phase, seen[`chat:${c.id}`] ?? 0, false);
    if (settledFor({ ...c, status }, now.value)) continue;
    out.push({
      key: `chat:${c.id}`, kind: "chat", id: c.id, title: c.title || `Chat ${c.id}`, workspace: wsName.get(c.workspaceId) ?? "",
      detail: status === "permission" || status === "waiting" ? (phase?.detail ?? "") : "",
      status, route: `/ws/${c.workspaceId}?chat=${c.id}`, recap: c.recap,
      at: Math.max(c.lastActivityAt ?? 0, phase?.updated_at ?? 0),
    });
  }
  for (const [ws, tabs] of Object.entries(s.tabs)) {
    for (const t of tabs as { pty_id?: number | null; title?: string | null; default_title?: string | null }[]) {
      if (!t.pty_id) continue;
      const phase = s.phases[`pty:${t.pty_id}`];
      if (!phase?.is_agent) continue;
      out.push({
        key: `pty:${t.pty_id}`, kind: "tab", id: t.pty_id, title: t.title || t.default_title || "Terminal", workspace: wsName.get(Number(ws)) ?? "",
        detail: "", status: displayStatus(phase, seen[String(t.pty_id)] ?? 0, false),
        route: `/ws/${ws}/tab/${t.pty_id}`, at: phase.updated_at ?? 0,
      });
    }
  }
  return out.sort((a, b) => b.at - a.at);
});

const needsYou = (st: TermStatus) => st === "permission" || st === "waiting" || st === "error";
const unread = (st: TermStatus) => st === "review" || st === "done";

const groups = computed(() => [
  { label: "Needs you", rows: rows.value.filter((r) => needsYou(r.status)) },
  { label: "Working", rows: rows.value.filter((r) => r.status === "running") },
  { label: "Unread", rows: rows.value.filter((r) => unread(r.status)) },
  // ponytail: idle threads capped at 5 — the panel is a glance, the Sidebar is the list.
  { label: "Recent", rows: rows.value.filter((r) => r.status === "idle").slice(0, 5) },
]);

const summary = computed(() => {
  const parts: string[] = [];
  const [needs, working, unreadRows] = groups.value;
  if (needs.rows.length) parts.push(`${needs.rows.length} waiting on you`);
  if (working.rows.length) parts.push(`${working.rows.length} working`);
  if (unreadRows.rows.length) parts.push(`${unreadRows.rows.length} unread`);
  return parts.length ? parts.join(" · ") : "All quiet";
});

const headerTitle = computed(() => {
  const v = view.value;
  if (v.kind === "chat") return chats.sessions.find((s) => s.id === v.chatId)?.title || "Chat";
  if (v.kind === "compose") return "New thread";
  return "Burrow";
});

function ago(ts: number): string {
  if (!ts) return "";
  const s = Math.max(0, Math.round((now.value - ts) / 1000));
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

function rightLabel(row: Row): string {
  switch (row.status) {
    case "permission": return "Needs input";
    case "waiting": return "Waiting";
    case "error": return "Error";
    default: return ago(row.at);
  }
}

function dotClass(status: TermStatus): string {
  switch (status) {
    case "running": return "bg-accent";
    case "permission":
    case "waiting": return "bg-[var(--yellow)]";
    case "error": return "bg-destructive";
    default: return "bg-[var(--blue)]";
  }
}

function textClass(status: TermStatus): string {
  return needsYou(status) ? (status === "error" ? "text-destructive" : "text-[var(--yellow)]") : "text-muted-foreground";
}

// ── PTY permissions ─────────────────────────────────────────────────────────
function permsFor(row: Row): PermRequest[] {
  return row.kind === "tab" ? perms.value.filter((p) => p.pty_id === String(row.id)) : [];
}
function permTarget(req: PermRequest): string {
  const i = req.tool_input ?? {};
  return String(i.command ?? i.file_path ?? i.path ?? i.url ?? i.pattern ?? "").slice(0, 200);
}
async function answer(req: PermRequest, behavior: "allow" | "always" | "deny") {
  perms.value = perms.value.filter((p) => p.id !== req.id);
  await invoke("answer_pty_permission", { id: req.id, behavior, message: "" }).catch(() => {});
  void load();
}

// ── Quick reply ─────────────────────────────────────────────────────────────
const replyTo = ref<number | null>(null);
const replyText = ref("");
const replyEl = ref<HTMLInputElement[] | null>(null);
function toggleReply(row: Row) {
  replyTo.value = replyTo.value === row.id ? null : row.id;
  replyText.value = "";
  void nextTick(() => replyEl.value?.[0]?.focus());
}
async function sendReply(row: Row) {
  const text = replyText.value.trim();
  if (!text) return;
  replyText.value = "";
  replyTo.value = null;
  // Always the queue: a running turn gets it when it settles, a settled chat
  // within ~300 ms (chatdelivery.go kicks a pass on every add).
  await invoke("chat_queue_add", { chatId: row.id, text, images: [] }).catch(() => {});
}

// ── Navigation ──────────────────────────────────────────────────────────────
function open(route: string) {
  void TrayOpen(route);
}
// AgentChat asks its host for the Right Panel; the panel has none. A sub-agent
// opens right here, a surface (plan, diff, git) in the main window.
provide("openRightPanelSubagent", (chatId: number) => { view.value = { kind: "chat", chatId }; });
provide("openRightPanelSurface", () => { const v = view.value; if (v.kind === "chat") open(chatRoute(v.chatId)); });
provide("openRightPanelGitTab", () => { const v = view.value; if (v.kind === "chat") open(chatRoute(v.chatId)); });

function openRow(row: Row) {
  if (row.kind === "chat") view.value = { kind: "chat", chatId: row.id };
  else open(row.route);
}

// ── New thread ──────────────────────────────────────────────────────────────
const composeWs = ref<string>();
const composeText = ref("");
const composeError = ref("");
const composing = ref(false);
const composeEl = ref<HTMLTextAreaElement | null>(null);
const wsOptions = computed(() =>
  workspaces.workspaces.filter((w) => !w.parent_id).map((w) => ({ value: String(w.id), label: w.name })),
);
function startCompose() {
  composeError.value = "";
  // Default: the project of the most recently active thread.
  const recent = rows.value.find((r) => r.kind === "chat");
  const recentWs = recent ? chats.sessions.find((s) => s.id === recent.id)?.workspaceId : undefined;
  composeWs.value ??= recentWs ? String(recentWs) : wsOptions.value[0]?.value;
  view.value = { kind: "compose" };
  void nextTick(() => composeEl.value?.focus());
}
async function sendCompose() {
  const text = composeText.value.trim();
  if (!text || !composeWs.value || composing.value) return;
  composing.value = true;
  composeError.value = "";
  try {
    // remote_create_chat: the row with default launch settings, and it leaves
    // chatActiveByWs (the main window's selection) alone.
    const row = await invoke<{ id: number }>("remote_create_chat", {
      workspaceId: Number(composeWs.value), agentKind: "claude", model: "", effort: "", permissionMode: "",
    });
    await chats.reload();
    await invoke("chat_send", { chatId: row.id, text, images: [] });
    nameThread(row.id, Number(composeWs.value), text);
    composeText.value = "";
    view.value = { kind: "chat", chatId: row.id };
  } catch (e) {
    composeError.value = String(e).slice(0, 120);
  } finally {
    composing.value = false;
  }
}

// remote_create_chat titles the row "Chat N (phone)"; give it the welcome
// composer's titles instead: the prompt heuristic now, a model's when it answers.
function nameThread(chatId: number, wsId: number, text: string) {
  const heuristic = smartTitle(text);
  chats.sync(chatId, { title: heuristic });
  const cwd = workspaces.workspaces.find((w) => w.id === wsId)?.path ?? "";
  void invoke<string>("generate_chat_title", { cwd, model: ui.textGenerationModel, policy: ui.textGenerationPolicy, text })
    .then((title) => {
      const s = chats.sessions.find((c) => c.id === chatId);
      if (title && s && !s.pinnedTitle && s.title === heuristic) chats.sync(chatId, { title });
    })
    .catch(() => {});
}

// ── Pinning + window size ───────────────────────────────────────────────────
// Unpinned, the window follows its content: the list hugs its rows (up to
// LIST_MAX_H), a chat or the composer gets a roomier fixed size. Pinned, the
// size is the user's (TraySetPinned makes it resizable) and is left alone.
const WIDTH = 340;
const LIST_MAX_H = 520;
const CHAT_SIZE = { w: 440, h: 640 };
const panel = ref<HTMLElement | null>(null);
const list = ref<HTMLElement | null>(null);
let lastSize = "";

async function setSize(w: number, h: number) {
  const key = `${w}x${h}`;
  if (key === lastSize) return;
  lastSize = key;
  await Window.SetSize(w, h).catch(() => {});
}
async function fitWindow() {
  if (pinned.value) return;
  if (view.value.kind !== "list") return setSize(CHAT_SIZE.w, CHAT_SIZE.h);
  await nextTick();
  if (!panel.value || !list.value) return;
  // CSS px inside #app are scaled by the UI-scale `zoom`; the window is not.
  const zoom = Number(getComputedStyle(document.getElementById("app") ?? document.body).zoom) || 1;
  const chrome = panel.value.offsetHeight - list.value.offsetHeight;
  await setSize(WIDTH, Math.min(LIST_MAX_H, Math.ceil((chrome + list.value.scrollHeight) * zoom)));
}
watch([rows, perms, view], fitWindow);

async function togglePin() {
  pinned.value = !pinned.value;
  if (pinned.value) {
    void TraySetPinned(true);
    void rememberGeometry();
    return;
  }
  saveGeometry({ pinned: false });
  // Unpin: back to the content size first (the user may have resized it),
  // then Go puts it back under the tray icon at that size.
  lastSize = "";
  await fitWindow();
  void TraySetPinned(false);
}

// ── A pinned panel survives a restart: where it was, how big ───────────────
// Per device (localStorage), like the read receipts.
const GEOMETRY_KEY = "burrow.trayPanel";
interface Geometry { pinned: boolean; x?: number; y?: number; w?: number; h?: number }
function loadGeometry(): Geometry {
  try {
    return JSON.parse(localStorage.getItem(GEOMETRY_KEY) ?? "{}") as Geometry;
  } catch {
    return { pinned: false };
  }
}
function saveGeometry(g: Geometry) {
  localStorage.setItem(GEOMETRY_KEY, JSON.stringify(g));
}
// ponytail: polled every 2 s while pinned (local window calls, no backend);
// a move/resize event would be exact if Wails' JS runtime ever exposes one.
async function rememberGeometry() {
  if (!pinned.value) return;
  const [pos, size] = await Promise.all([Window.Position(), Window.Size()]).catch(() => [null, null] as const);
  if (!pos || !size) return;
  const g = { pinned: true, x: pos.x, y: pos.y, w: size.width, h: size.height };
  if (JSON.stringify(g) !== localStorage.getItem(GEOMETRY_KEY)) saveGeometry(g);
}
async function restoreGeometry() {
  const g = loadGeometry();
  if (!g.pinned) return void TraySetPinned(false); // a reload must not leave Go thinking it is pinned
  pinned.value = true;
  await TraySetPinned(true);
  if (g.w && g.h) await Window.SetSize(g.w, g.h).catch(() => {});
  if (g.x != null && g.y != null) await Window.SetPosition(g.x, g.y).catch(() => {});
}

// ── Live state ──────────────────────────────────────────────────────────────
// One shell_snapshot for the first paint, then the phase events of exactly the
// chats and agent tabs on the list, plus pty-permissions. A slow re-read only
// catches what has no event of its own here (a tab opened, a workspace renamed).
async function load() {
  if (document.visibilityState === "hidden") return;
  now.value = Date.now();
  const [s, p] = await Promise.all([
    invoke<ShellSnapshotData>("shell_snapshot").catch(() => snap.value),
    invoke<PermRequest[]>("list_pty_permissions").catch(() => perms.value),
  ]);
  snap.value = s;
  perms.value = p ?? [];
}

const phaseKeys = computed(() => {
  const keys = chats.sessions.filter((c) => !c.archivedAt && !c.parentChatId && c.kind !== "side").map((c) => `chat:${c.id}`);
  for (const tabs of Object.values(snap.value?.tabs ?? {})) {
    for (const t of tabs as { pty_id?: number | null }[]) if (t.pty_id) keys.push(`pty:${t.pty_id}`);
  }
  return keys;
});
const phaseListeners = new Map<string, Promise<() => void>>();
watch(phaseKeys, (keys) => {
  const want = new Set(keys);
  for (const [key, off] of phaseListeners) {
    if (!want.has(key)) { void off.then((f) => f()); phaseListeners.delete(key); }
  }
  for (const key of want) {
    if (phaseListeners.has(key)) continue;
    phaseListeners.set(key, listen<Phase>(`phase-${key}`, (e) => {
      if (!snap.value || !e.payload) return;
      now.value = Date.now();
      snap.value = { ...snap.value, phases: { ...snap.value.phases, [key]: e.payload } };
    }));
  }
}, { immediate: true });

function onKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape" || e.defaultPrevented) return;
  if (view.value.kind !== "list") view.value = { kind: "list" };
  else if (!pinned.value) void Window.Hide();
}
let resync: number | undefined;
let geometryTimer: number | undefined;
const unlisteners: (() => void)[] = [];
onMounted(async () => {
  document.documentElement.style.background = "transparent";
  document.body.style.background = "transparent";
  void restoreGeometry();
  void load();
  resync = window.setInterval(load, 15_000);
  geometryTimer = window.setInterval(rememberGeometry, 2000);
  window.addEventListener("focus", load);
  window.addEventListener("keydown", onKeydown);
  // ⌥Space (trayhotkey_darwin.go): straight to a new thread.
  unlisteners.push(await listen("menu-tray-compose", () => startCompose()));
  unlisteners.push(await listen("pty-permissions", () => {
    void invoke<PermRequest[]>("list_pty_permissions").then((p) => { perms.value = p ?? []; }).catch(() => {});
  }));
  await workspaces.load().catch(() => {});
  wsReady.value = true;
});
onBeforeUnmount(() => {
  clearInterval(resync);
  clearInterval(geometryTimer);
  window.removeEventListener("focus", load);
  window.removeEventListener("keydown", onKeydown);
  unlisteners.forEach((f) => f());
  for (const off of phaseListeners.values()) void off.then((f) => f());
});
</script>

<style scoped>
.tray-panel {
  background: color-mix(in srgb, var(--sidebar-bg) 82%, transparent);
}
.tray-icon-btn {
  --wails-draggable: no-drag;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 6px;
  color: var(--muted-foreground);
  transition: background-color 0.15s, color 0.15s;
}
.tray-icon-btn:hover {
  background: var(--hover);
  color: var(--foreground);
}
.tray-chip {
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--secondary-foreground);
  transition: background-color 0.15s, color 0.15s, opacity 0.15s;
}
/* The thread in the panel is the full AgentChat with a tighter composer and
   transcript margins — styled from here, AgentChat has no "dense" prop. */
.tray-chat :deep(.chat-messages) {
  padding-top: 10px;
}
.tray-chat :deep(.chat-main > .flex-shrink-0.pb-2) {
  padding: 6px 8px 6px;
}
.tray-chat :deep(.chat-input-box) {
  border-radius: 12px;
}
.tray-chat :deep(.composer-input) {
  min-height: 2.25rem;
  padding: 7px 12px 2px;
  font-size: 13px;
}
.tray-chat :deep(.composer-toolbar) {
  gap: 4px;
  padding: 2px 6px 5px;
}
.tray-chat :deep(.composer-pill) {
  height: 24px;
  padding: 0 6px;
  font-size: 11.5px;
}
.tray-chat :deep(.composer-pill-label) {
  font-size: 11.5px;
}
</style>
