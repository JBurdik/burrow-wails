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
                  <img v-if="row.icon" :src="row.icon" class="size-4 rounded-[4px] object-cover" />
                  <component :is="row.kind === 'chat' ? PhChatCircle : PhTerminalWindow" v-else :size="15" class="text-muted-foreground" />
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
              <!-- A chat's pending request, answered through its own session (same rules and transcript row as the chat). -->
              <div v-for="(req, i) in chatPending(row)" :key="row.key + ':' + i" class="mx-2 mb-1.5 ml-[34px] rounded-lg border border-border px-2.5 py-2">
                <template v-if="req.kind === 'permission' || req.kind === 'diff'">
                  <div class="truncate font-mono text-[11px] text-secondary-foreground" :title="toolTarget(req.request.input)">{{ req.request.toolName }}<template v-if="toolTarget(req.request.input)">: {{ toolTarget(req.request.input) }}</template></div>
                  <div class="mt-1.5 flex gap-1.5">
                    <button type="button" class="tray-chip bg-accent text-accent-foreground hover:opacity-90" @click="answerChat(row.id, { kind: 'permission', allow: true })">Allow</button>
                    <button type="button" class="tray-chip hover:bg-hover hover:text-foreground" @click="answerChat(row.id, { kind: 'permission', allow: true, always: true })">Always</button>
                    <button type="button" class="tray-chip hover:bg-hover hover:text-destructive" @click="answerChat(row.id, { kind: 'permission', allow: false })">Deny</button>
                  </div>
                </template>
                <template v-else-if="req.kind === 'acpPermission'">
                  <div class="truncate text-[11px] text-secondary-foreground" :title="req.request.title">{{ req.request.title }}</div>
                  <div class="mt-1.5 flex flex-wrap gap-1.5">
                    <button
                      v-for="o in req.request.options"
                      :key="o.optionId"
                      type="button"
                      class="tray-chip"
                      :class="o.kind.startsWith('reject') ? 'hover:bg-hover hover:text-destructive' : o.kind === 'allow_once' ? 'bg-accent text-accent-foreground hover:opacity-90' : 'hover:bg-hover hover:text-foreground'"
                      @click="answerChat(row.id, { kind: 'acpOption', optionId: o.optionId, label: o.name, reject: o.kind.startsWith('reject') })"
                    >{{ o.name }}</button>
                  </div>
                </template>
                <!-- A question / plan / Codex input needs the full form: open the thread. -->
                <button v-else type="button" class="tray-chip hover:bg-hover hover:text-foreground" @click="openRow(row)">
                  {{ req.kind === 'plan' ? 'Review the plan…' : 'Answer…' }}
                </button>
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
import type { TermStatus } from "@/lib/terminalStatus";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { Select } from "@/components/ui/select";
import { chatSession, TRANSCRIPT_PAGE, type ChatMessagesPage, type ChatSession, type PendingAnswer, type PendingRequest } from "@/lib/chatSession";
import { useUIStore } from "@/stores/ui";
import { smartTitle } from "@/lib/chatTitle";
import { feedGroups, needsYou, permTarget, useThreadFeed, type FeedRow } from "@/lib/threadFeed";

const AgentChat = defineAsyncComponent(() => import("@/components/AgentChat.vue"));

type View = { kind: "list" } | { kind: "chat"; chatId: number } | { kind: "compose" };

const chats = useClaudeChatsStore();
const workspaces = useWorkspaceStore();
const ui = useUIStore();
const { rows, perms, now, answerPerm: answer, permsFor } = useThreadFeed();
const view = ref<View>({ kind: "list" });
const pinned = ref(false);
const wsReady = ref(false);

/** The panel fills the window (pinned, or showing a chat / composer); else it hugs the list. */
const fill = computed(() => pinned.value || view.value.kind !== "list");

const chatRoute = (id: number) => {
  const c = chats.sessions.find((s) => s.id === id);
  return c ? `/ws/${c.workspaceId}?chat=${id}` : "";
};
const chatWs = (id: number) => {
  const c = chats.sessions.find((s) => s.id === id);
  return c ? workspaces.workspaces.find((w) => w.id === c.workspaceId) : undefined;
};

// ponytail: idle threads capped at 5 — the panel is a glance, the Sidebar is the list.
const groups = computed(() => feedGroups(rows.value, 5));

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

function rightLabel(row: FeedRow): string {
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
// ── Chat requests (permission / question / plan) ────────────────────────────
// Only Go's phase knows a chat is blocked; the request itself lives in the
// chat's session (chatSession.ts). For a row that needs the user the panel
// holds that session — the same one a mounted AgentChat would use — so an
// answer goes through respond(): same "Always" rules, same transcript row.
//
// LOAD-BEARING: the session must hold the transcript before it can answer.
// respond() appends a row and save() replaces the whole stored transcript, so
// an empty, unhydrated session would wipe the chat's history. A failed or
// empty read ⇒ no inline answer, the row only opens the thread.
const held = new Map<number, ChatSession>();
const answerable = ref(new Set<number>());
async function holdSession(chatId: number) {
  if (held.has(chatId)) return;
  const S = chatSession(chatId);
  S.retain();
  held.set(chatId, S);
  if (S.messages.value.length === 0) {
    const page = await invoke<ChatMessagesPage>("load_chat_messages_page", { chatId, before: -1, limit: TRANSCRIPT_PAGE }).catch(() => null);
    if (!held.has(chatId)) return; // released while loading
    if (!page || !Array.isArray(page.messages) || (page.messages.length === 0 && page.start === 0)) return;
    if (S.messages.value.length === 0) S.hydrate({ messages: page.messages, hiddenBefore: page.start });
  }
  await S.listenClaude();
  await S.listenAcp();
  await S.recoverPendingRequests();
  if (held.has(chatId)) answerable.value = new Set([...answerable.value, chatId]);
}
function releaseSession(chatId: number) {
  held.get(chatId)?.release();
  held.delete(chatId);
  if (answerable.value.has(chatId)) answerable.value = new Set([...answerable.value].filter((id) => id !== chatId));
}
const blockedChats = computed(() => rows.value.filter((r) => r.kind === "chat" && (r.status === "permission" || r.status === "waiting")).map((r) => r.id));
watch(blockedChats, (ids) => {
  const want = new Set(ids);
  for (const id of [...held.keys()]) if (!want.has(id)) releaseSession(id);
  for (const id of want) void holdSession(id);
}, { immediate: true });

function chatPending(row: FeedRow): readonly PendingRequest[] {
  if (row.kind !== "chat" || !answerable.value.has(row.id)) return [];
  return held.get(row.id)?.pendingRequests.value ?? [];
}
function toolTarget(input: Record<string, unknown>): string {
  return String(input.command ?? input.file_path ?? input.path ?? input.url ?? input.pattern ?? "").slice(0, 200);
}
async function answerChat(chatId: number, answer: PendingAnswer) {
  await held.get(chatId)?.respond(answer);
}

// ── Quick reply ─────────────────────────────────────────────────────────────
const replyTo = ref<number | null>(null);
const replyText = ref("");
const replyEl = ref<HTMLInputElement[] | null>(null);
function toggleReply(row: FeedRow) {
  replyTo.value = replyTo.value === row.id ? null : row.id;
  replyText.value = "";
  void nextTick(() => replyEl.value?.[0]?.focus());
}
async function sendReply(row: FeedRow) {
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

function openRow(row: FeedRow) {
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

function onKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape" || e.defaultPrevented) return;
  if (view.value.kind !== "list") view.value = { kind: "list" };
  else if (!pinned.value) void Window.Hide();
}
let geometryTimer: number | undefined;
const unlisteners: (() => void)[] = [];
onMounted(async () => {
  document.documentElement.style.background = "transparent";
  document.body.style.background = "transparent";
  void restoreGeometry();
  geometryTimer = window.setInterval(rememberGeometry, 2000);
  window.addEventListener("keydown", onKeydown);
  // ⌥Space (trayhotkey_darwin.go): straight to a new thread.
  unlisteners.push(await listen("menu-tray-compose", () => startCompose()));
  await workspaces.load().catch(() => {});
  wsReady.value = true;
});
onBeforeUnmount(() => {
  clearInterval(geometryTimer);
  window.removeEventListener("keydown", onKeydown);
  unlisteners.forEach((f) => f());
  for (const id of [...held.keys()]) releaseSession(id);
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
