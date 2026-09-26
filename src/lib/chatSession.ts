import { computed, effectScope, nextTick, ref, watch, type ComputedRef, type EffectScope, type Ref } from "vue";
import { listen as tauriListen, type UnlistenFn } from "@tauri-apps/api/event";
import { invoke as tauriInvoke } from "@tauri-apps/api/core";
import type {
  AcpConfigOption, AcpModes, AcpPermReq, CanUseToolReq, ChatMessage, CodexUserInputReq, QueuedChatMessage, TurnStats,
} from "@/lib/chatTypes";
import {
  applyChatEvent, isProjectedEvent, settleTranscript, type ChatEventBatch, type ChatProjectionState,
} from "@/lib/chatProjection";
import { parseAcpPermRequest } from "@/lib/acpParser";
import { parseContextReport, type CtxReportRow } from "@/lib/contextReport";
import { appChatHost, type ChatHost } from "@/lib/chatHost";

// Who owns a chat's stream — the answer used to be "whichever AgentChat.vue is
// mounted", which is what made the component un-unmountable: its
// onBeforeUnmount dropped the `claude-data-{id}` listener while the agent kept
// talking, so anything said in between was gone (docs/plans/003-view-state-routes.md).
//
// Ported from t3code's `threadDetailSubscriptions` (apps/web/src/environments/
// runtime/service.ts): a registry keyed by thread, refcounted by mounted views,
// and — the part that matters — `shouldEvictThreadDetailSubscription` refuses to
// tear a subscription down while the thread is non-idle, even at refCount 0.
// The view becomes a reader; the stream outlives it.
//
// The session also REDUCES the stream. The reducers used to live in
// AgentChat.vue and were installed here as closures that outlived the
// component; state they touched but the session did not own (a Codex
// user-input prompt, for one) was written into a dead instance. Now every
// piece of state a line can change is here, and the view reads it.
//
// ponytail: what stays in the component is what is tied to the DOM or to the
// composer (scroll element, menus, draft text, question-wizard answers, the
// model/profile pickers). The session reaches those through ChatViewHooks.

/**
 * What the session asks of a view. The LAST attached view's hooks stay
 * installed after it unmounts — a turn finishing behind a closed view still
 * restores ACP selections and drains its queue through them — so a hook must
 * be safe to call on an unmounted instance (scrolling a detached element is).
 */
export interface ChatViewHooks {
  scrollToBottom(force?: boolean): void;
  /** Reset the question wizard's local answers for a new AskUserQuestion. */
  onQuestionOpened(): void;
  /** Reset the plan-feedback draft for a new ExitPlanMode. */
  onPlanOpened(): void;
  /** An ACP handshake landed (the model catalog is worth learning). */
  onAcpSession(): void;
  /** The adapter reset its selectors — push the user's picks back. */
  restoreAcpSelections(): void;
  /** True for ACP / Codex, false for the native Claude transport. */
  usesRpcRuntime(): boolean;
  /** Send a follow-up as its own turn (the queue drain). */
  send(text: string, images?: string[]): Promise<void>;
}

/** The session's outside world. Injected so tests can run it with no Tauri, no Pinia. */
export interface ChatSessionDeps {
  invoke: typeof tauriInvoke;
  listen: typeof tauriListen;
  host: ChatHost;
}

const appDeps: ChatSessionDeps = { invoke: tauriInvoke, listen: tauriListen, host: appChatHost };

/** Match keys for "Allow always" rules. Bash gets a command-prefix key so
 *  allowing `git` once doesn't blanket-allow every Bash call. */
export function ruleKeys(toolName: string, input: Record<string, unknown>): string[] {
  const keys = [toolName];
  if (toolName === "Bash" && typeof input.command === "string") {
    const first = (input.command as string).trim().split(/\s+/)[0];
    if (first) keys.push(`Bash:${first}`);
  }
  return keys;
}

export type CtxSplit = { cached: number; fresh: number; output: number };

// Remembered per chat, because replayChatStream only replays from folded_ord —
// the usage that filled the context ring is usually behind that mark, so
// nothing would re-emit it and the ring would vanish until the next turn.
// Stale after an external /clear, and self-healing on the next turn.
// ponytail: one unpruned map; revisit if someone keeps thousands of chats.
const CTX_STORE_KEY = "burrow.ctxTokens";
type CtxEntry = { tokens: number; window: number; split?: CtxSplit | null };
function readCtxStore(): Record<string, CtxEntry> {
  try { return JSON.parse(localStorage.getItem(CTX_STORE_KEY) || "{}"); } catch { return {}; }
}

function hasFocus(): boolean {
  return typeof document !== "undefined" && document.hasFocus();
}

export interface ChatSession {
  readonly chatId: number;

  // ── transcript ──
  messages: Ref<ChatMessage[]>;
  /** Monotonic id generator for messages; plain field, never rendered. */
  nextMsgId: number;
  /**
   * Highest chat_stream ord this session has actually put through a reducer.
   * `foldedOrd` handed to save_chat_messages is this + 1 — "everything up to
   * and including lastOrd is now in chat_messages", which is what lets the
   * backend trim safely (chatstream.go).
   */
  lastOrd: number;
  /** True once a restart replay has run (or been ruled out) for this session. */
  replayed: boolean;

  // ── turn state ──
  busy: Ref<boolean>;
  lastActivityAt: Ref<number>;
  turnStartedAt: Ref<number>;
  /**
   * FIFO of follow-ups, DERIVED from the transcript's own `queued` entries.
   * There is deliberately no second array: the queue and its placeholder
   * bubbles used to be two structures synchronised by hand in five methods,
   * and `moveQueuedMessageNext` only ever updated one of them — "Send Next"
   * reordered what was sent while the transcript kept showing the old order.
   * Deriving it also makes the queue survive a relaunch for free, since the
   * placeholders are persisted with the transcript.
   */
  messageQueue: ComputedRef<QueuedChatMessage[]>;
  enqueueMessage(text: string, images?: string[]): QueuedChatMessage;
  removeQueuedMessage(id: number): void;
  clearQueuedMessages(): void;
  moveQueuedMessageNext(id: number): void;
  takeNextQueuedMessage(): QueuedChatMessage | undefined;
  suppressNextDone: Ref<boolean>;
  sessionId: Ref<string>;
  turnStats: Ref<TurnStats | null>;
  sessionCost: Ref<number>;
  runtimeStarted: Ref<boolean>;

  // ── context meter + /context report ──
  contextTokens: Ref<number>;
  contextWindow: Ref<number>;
  contextSplit: Ref<CtxSplit | null>;
  contextReport: Ref<CtxReportRow[] | null>;
  contextReportPending: Ref<boolean>;
  /** A `/context` was sent out of band; its report is lifted out of the stream. */
  awaitingContextReport: Ref<boolean>;
  /** Claude named the thread — later heuristics must not overwrite it. */
  claudeGeneratedTitle: Ref<boolean>;

  // ── blocking requests (native Claude transport) ──
  pendingPermission: Ref<CanUseToolReq | null>;
  pendingQuestion: Ref<CanUseToolReq | null>;
  pendingPlan: Ref<CanUseToolReq | null>;
  pendingDiff: Ref<CanUseToolReq | null>;
  pendingPermissionMsgId: Ref<number | null>;
  pendingQuestionMsgId: Ref<number | null>;
  pendingPlanMsgId: Ref<number | null>;
  pendingDiffMsgId: Ref<number | null>;
  settledControlRequestIds: Set<string>;
  /** A native control response is being written; the prompt stays until it lands. */
  nativeControlResponsePending: Ref<boolean>;

  // ── blocking requests (ACP transport) ──
  acpPermReq: Ref<AcpPermReq | null>;
  /** JSON-RPC id of the pending request. Lives here, not in the view: the
   *  `serverRequest/resolved` that clears the prompt can arrive after a
   *  remount, and a fresh view's local id would never match it. */
  acpPermRpcId: Ref<number | null>;
  acpPermMsgId: Ref<number | null>;
  acpPromptRpcId: Ref<number | null>;
  /** An answer to acpPermReq is in flight (Codex clears it on serverRequest/resolved). */
  permissionResponsePending: Ref<boolean>;
  /** Codex `item/tool/requestUserInput`: blocking, so it must outlive the view. */
  codexUserInput: Ref<CodexUserInputReq | null>;
  codexUserInputPending: Ref<boolean>;
  acpControlIds: Set<number>;
  /** Ids of OUR OWN selector restore pushes — their replies must not restore again. */
  acpRestorePushIds: Set<number>;
  acpModes: Ref<AcpModes | null>;
  acpConfigOptions: Ref<AcpConfigOption[]>;

  /** Every request the agent is blocked on, whatever the transport. */
  pendingRequests: ComputedRef<unknown[]>;

  /** Point the session's hooks at this view. Replaces any previous view's. */
  attachView(view: ChatViewHooks): void;
  /** A prompt this client is about to send — drop its echo when Go publishes it. */
  expectEcho(text: string): void;
  /** Persist the transcript (fire-and-forget), with the folded stream mark. */
  save(): void;
  /** Mirror busy / message count into the chat list and the remote mirror. */
  sync(): void;
  /** Settle partial rows and stuck tool spinners (a turn ended somehow). */
  settleTranscript(failedTools?: boolean): void;
  /** Remove a system-info marker row by id. */
  removeFeedMarker(id: number | null): void;
  /** Record a native control request as answered (a replay must not reopen it). */
  settleControlRequest(requestId: string): void;
  /** Answer a native can_use_tool request. Throws if the write failed. */
  respondControl(requestId: string, response: Record<string, unknown>): Promise<void>;
  /** Send the next queued follow-up if the chat is idle. */
  drain(): void;
  /** Attach the domain-event listener (idempotent). Both transports use it. */
  listenEvents(): Promise<void>;
  /** Attach the native Claude stream listener (idempotent). */
  listenClaude(): Promise<void>;
  /** Attach the ACP data + request listeners (idempotent). */
  listenAcp(): Promise<void>;
  /** Drop every listener — used when the chat itself goes away. */
  detach(): void;

  retain(): void;
  release(): void;
  /**
   * Re-check whether this session is still worth keeping.
   *
   * release() cannot be the only place that decides: a session that was BUSY
   * when its view closed is deliberately kept, and when that turn later
   * finishes nothing looks again — so it would sit in the registry with its
   * listeners and its whole transcript until the chat itself is closed. Call
   * this at a turn boundary.
   */
  maybeEvict(): void;
  /**
   * Is any mounted view holding this chat right now?
   *
   * The component must NOT answer this from its own props once it can be
   * unmounted: a prop is frozen at unmount, so a turn finishing behind a
   * closed view would report itself as watched and settle to a transient
   * `done` that clears itself — the very bug fáze 3 exists to prevent.
   */
  isWatched(): boolean;
}

interface InternalSession extends ChatSession {
  deps: ChatSessionDeps;
  view: ChatViewHooks | null;
  /** Prompts this client sent and has not yet seen echoed back (see expectEcho). */
  pendingSends: Set<string>;
  /** Detached scope for the session's own watchers, so `create()` being called
   *  from a view's setup() does not hand its effects to that view's scope —
   *  they would then be disposed on unmount, which is the whole bug. */
  scope: EffectScope;
  refCount: number;
  evictTimer: ReturnType<typeof setTimeout> | null;
  eventsUL: UnlistenFn | null;
  claudeUL: UnlistenFn | null;
  acpDataUL: UnlistenFn | null;
  acpReqUL: UnlistenFn | null;
}

/** A transcript entry read back as a queue entry; [] for anything else. */
function queuedEntry(message: ChatMessage): QueuedChatMessage[] {
  if (message.role !== "queued") return [];
  return [{ id: message.id, text: message.text, ...(message.images?.length ? { images: message.images } : {}) }];
}

const sessions = new Map<number, InternalSession>();

/** What the Go side puts on `claude-data-*` / `acp-data-*` / `acp-req-*`. */
export interface StreamEvent { ord: number; kind: string; line: string }

/** Record the ord, then reduce the bare line. */
function feed(s: InternalSession, ev: StreamEvent, reduce: (s: InternalSession, line: string) => void) {
  if (ev.ord > s.lastOrd) s.lastOrd = ev.ord;
  reduce(s, ev.line);
}

/**
 * Re-feed the lines that arrived after the last transcript save — the case
 * being covered is an app restart (or crash) mid-turn, where the session is
 * new but the agent's output is already recorded in chat_stream.
 *
 * Only runs when the backend has a folded mark: without one, "replay from 0"
 * would re-play a whole history that chat_messages already holds. Chats from
 * before folded_ord existed therefore behave exactly as they did.
 */
export async function replayChatStream(chatId: number): Promise<void> {
  const s = sessions.get(chatId);
  if (!s || s.replayed) return;
  s.replayed = true;
  try {
    const folded = await s.deps.invoke<number>("chat_folded_ord", { chatId });
    if (!folded) return;
    // Domain events, not raw lines: a replay should rebuild the transcript and
    // nothing else. Re-feeding raw would also re-open permission requests that
    // were answered before the restart.
    const batches = await s.deps.invoke<ChatEventBatch[]>("load_chat_events_since", { chatId, since: folded });
    for (const batch of batches) {
      if (batch.ord > s.lastOrd) s.lastOrd = batch.ord;
      onEvents(s, batch);
    }
  } catch {
    // Replay is best-effort recovery: a chat that cannot be caught up is still
    // usable, and the next save re-anchors folded_ord.
  }
}

/**
 * A session is idle when nothing is in flight and nothing is waiting on the
 * user. Only an idle session may be evicted once no view holds it — this is
 * t3code's `shouldEvictThreadDetailSubscription`, and it is the whole reason a
 * running turn survives the user switching tabs.
 */
/** Delay before an unwatched, settled session is dropped. */
const EVICT_DELAY_MS = 2_000;

/** Nobody is looking, nothing is in flight, nothing is waiting to be sent. */
function evictable(s: InternalSession): boolean {
  return s.refCount === 0 && isIdle(s) && s.messageQueue.value.length === 0;
}

function isIdle(s: InternalSession): boolean {
  return !s.busy.value && s.pendingRequests.value.length === 0;
}

function create(chatId: number, deps: ChatSessionDeps): InternalSession {
  const scope = effectScope(true);
  const ctx = readCtxStore()[String(chatId)];
  const s: InternalSession = {
    chatId,
    deps,
    view: null,
    pendingSends: new Set<string>(),
    messages: ref<ChatMessage[]>([]),
    nextMsgId: 0,
    lastOrd: -1,
    replayed: false,

    busy: ref(false),
    lastActivityAt: ref(Date.now()),
    turnStartedAt: ref(0),
    messageQueue: computed(() => s.messages.value.flatMap(queuedEntry)),
    suppressNextDone: ref(false),
    sessionId: ref(""),
    turnStats: ref<TurnStats | null>(null),
    sessionCost: ref(0),
    runtimeStarted: ref(false),

    contextTokens: ref(ctx?.tokens ?? 0),
    contextWindow: ref(ctx?.window ?? 0),
    contextSplit: ref<CtxSplit | null>(ctx?.split ?? null),
    contextReport: ref<CtxReportRow[] | null>(null),
    contextReportPending: ref(false),
    awaitingContextReport: ref(false),
    claudeGeneratedTitle: ref(false),

    pendingPermission: ref<CanUseToolReq | null>(null),
    pendingQuestion: ref<CanUseToolReq | null>(null),
    pendingPlan: ref<CanUseToolReq | null>(null),
    pendingDiff: ref<CanUseToolReq | null>(null),
    pendingPermissionMsgId: ref<number | null>(null),
    pendingQuestionMsgId: ref<number | null>(null),
    pendingPlanMsgId: ref<number | null>(null),
    pendingDiffMsgId: ref<number | null>(null),
    settledControlRequestIds: new Set<string>(),
    nativeControlResponsePending: ref(false),

    acpPermReq: ref<AcpPermReq | null>(null),
    acpPermRpcId: ref<number | null>(null),
    acpPermMsgId: ref<number | null>(null),
    acpPromptRpcId: ref<number | null>(null),
    permissionResponsePending: ref(false),
    codexUserInput: ref<CodexUserInputReq | null>(null),
    codexUserInputPending: ref(false),
    acpControlIds: new Set<number>(),
    acpRestorePushIds: new Set<number>(),
    acpModes: ref<AcpModes | null>(null),
    acpConfigOptions: ref<AcpConfigOption[]>([]),

    pendingRequests: computed(() => [
      s.pendingPermission.value, s.pendingQuestion.value, s.pendingPlan.value, s.pendingDiff.value,
      s.acpPermReq.value, s.codexUserInput.value,
    ].filter((request) => request !== null)),

    enqueueMessage(text, images) {
      const entry: QueuedChatMessage = { id: s.nextMsgId++, text, ...(images?.length ? { images } : {}) };
      s.messages.value.push({ id: entry.id, role: "queued", text, ...(entry.images ? { images: entry.images } : {}) });
      return entry;
    },
    removeQueuedMessage(id) {
      s.messages.value = s.messages.value.filter((message) => message.id !== id || message.role !== "queued");
    },
    clearQueuedMessages() {
      s.messages.value = s.messages.value.filter((message) => message.role !== "queued");
    },
    moveQueuedMessageNext(id) {
      const messages = s.messages.value;
      const from = messages.findIndex((m) => m.id === id && m.role === "queued");
      const head = messages.findIndex((m) => m.role === "queued");
      if (from < 0 || from === head) return;
      const [entry] = messages.splice(from, 1);
      messages.splice(head, 0, entry);
    },
    takeNextQueuedMessage() {
      const index = s.messages.value.findIndex((m) => m.role === "queued");
      if (index < 0) return undefined;
      const [message] = s.messages.value.splice(index, 1);
      return queuedEntry(message)[0];
    },

    scope,
    refCount: 0,
    evictTimer: null,
    eventsUL: null,
    claudeUL: null,
    acpDataUL: null,
    acpReqUL: null,

    attachView(view) {
      s.view = view;
    },
    expectEcho(text) {
      s.pendingSends.add(text);
    },
    save() {
      // Partial messages are mid-stream and get re-sent; the cap only guards
      // against a pathological chat, not storage (a row per message is cheap).
      const toSave = s.messages.value.filter((m) => !m.partial).slice(-2000);
      // foldedOrd: this transcript accounts for every stream line up to
      // lastOrd, which is what makes the chat_stream trim safe (chatstream.go).
      // -1 while nothing has streamed yet: "don't move the mark".
      const foldedOrd = s.lastOrd >= 0 ? s.lastOrd + 1 : -1;
      void s.deps.invoke("save_chat_messages", { chatId, messages: JSON.stringify(toSave), foldedOrd }).catch(() => {});
    },
    sync() {
      s.deps.host.syncChat(chatId, {
        busy: s.busy.value,
        messageCount: s.messages.value.filter((m) => m.role !== "tool").length,
      });
      // The remote mirror is discovery + reconnect history only; incremental
      // updates travel over the stream itself.
      const chat = s.deps.host.chat(chatId);
      if (!chat) return;
      s.deps.invoke("remote_sync_chat", {
        chat: {
          id: chatId,
          workspaceId: chat.workspaceId,
          title: chat.title,
          busy: s.busy.value,
          status: chat.status ?? null,
          agentKind: chat.agentKind ?? null,
          transport: chat.transport ?? "claude-cli",
          claudeSessionId: s.sessionId.value,
          messages: s.messages.value.filter((message) => !message.partial).slice(-200),
        },
      }).catch(() => {}); // best-effort: a failed push must never disrupt the UI
    },
    settleTranscript(failedTools = false) {
      settleTranscript(projectionOf(s));
      // Safety net for a tool row that never got its matching update — a
      // dropped status line otherwise leaves the spinner running forever.
      for (const m of s.messages.value) {
        if (m.role === "tool" && m.toolOutput === undefined) {
          m.toolOutput = "";
          if (failedTools) m.toolFailed = true;
        }
      }
    },
    removeFeedMarker(id) {
      if (id === null) return;
      const idx = s.messages.value.findIndex((m) => m.id === id);
      if (idx !== -1) s.messages.value.splice(idx, 1);
    },
    settleControlRequest(requestId) {
      if (!requestId) return;
      s.settledControlRequestIds.add(requestId);
      // Ids are unique per process; keep enough to cover a reconnect without
      // growing a long-lived chat indefinitely.
      if (s.settledControlRequestIds.size > 200) {
        const oldest = s.settledControlRequestIds.values().next().value;
        if (oldest) s.settledControlRequestIds.delete(oldest);
      }
    },
    async respondControl(requestId, response) {
      await s.deps.invoke("claude_respond_control", { id: chatId, requestId, response });
      s.settleControlRequest(requestId);
      s.sync();
    },
    drain() {
      if (s.busy.value || !s.view) return;
      // Let the finishing turn settle its transcript, status and provider
      // correlation first, then re-check: Claude can resume the same session
      // on its own in that gap (markActive), and taking the message before the
      // gap meant the re-queue on the far side put it back at the TAIL.
      nextTick(() => {
        if (s.busy.value || !s.view) return;
        const next = s.takeNextQueuedMessage();
        if (!next) return;
        s.save();
        void s.view.send(next.text, next.images);
      });
    },
    async listenEvents() {
      if (!s.eventsUL) {
        s.eventsUL = await s.deps.listen<ChatEventBatch>(`chat-event-${chatId}`, (e) => {
          if (e.payload.ord > s.lastOrd) s.lastOrd = e.payload.ord;
          onEvents(s, e.payload);
        });
      }
    },
    async listenClaude() {
      if (!s.claudeUL) {
        s.claudeUL = await s.deps.listen<StreamEvent>(`claude-data-${chatId}`, (e) => feed(s, e.payload, onLine));
      }
    },
    async listenAcp() {
      if (!s.acpDataUL) {
        s.acpDataUL = await s.deps.listen<StreamEvent>(`acp-data-${chatId}`, (e) => feed(s, e.payload, onAcpData));
      }
      if (!s.acpReqUL) {
        s.acpReqUL = await s.deps.listen<StreamEvent>(`acp-req-${chatId}`, (e) => feed(s, e.payload, onAcpReq));
      }
    },
    detach() {
      if (s.evictTimer) { clearTimeout(s.evictTimer); s.evictTimer = null; }
      s.eventsUL?.(); s.eventsUL = null;
      s.claudeUL?.(); s.claudeUL = null;
      s.acpDataUL?.(); s.acpDataUL = null;
      s.acpReqUL?.(); s.acpReqUL = null;
      s.view = null;
      s.scope.stop();
    },

    isWatched() { return s.refCount > 0; },

    maybeEvict() {
      if (s.evictTimer) clearTimeout(s.evictTimer);
      // Deferred, and re-checked when it fires: a turn boundary is immediately
      // followed by the queued-message flush (a nextTick away), and evicting
      // between the two would hand that send a detached session while the next
      // mount built a fresh one.
      s.evictTimer = setTimeout(() => {
        s.evictTimer = null;
        if (!evictable(s)) return;
        s.detach();
        sessions.delete(chatId);
      }, EVICT_DELAY_MS);
    },

    retain() { s.refCount++; },
    release() {
      s.refCount = Math.max(0, s.refCount - 1);
      if (evictable(s)) {
        s.detach();
        sessions.delete(chatId);
      }
      // Non-idle and unwatched: keep listening. The turn finishes into this
      // session, and the next mount finds the result already here.
    },
  };
  // A turn ending is the ONLY thing that releases the queue, so watch the flag
  // rather than calling the drain from each place that clears it: `session.exited`
  // (the CLI died mid-turn) and the send-failure branches also clear `busy`.
  // Owned here, not by a watcher in the view: a chat leaf unmounts the moment
  // the user looks at another tab, and a component watcher died with it — the
  // queue then sat parked behind a send button the queue itself disables.
  scope.run(() => watch(s.busy, (running) => { if (!running) s.drain(); }));
  return s;
}

/**
 * The session for a chat, created on first use. Does not retain. `deps` only
 * matters on the call that creates it (tests); the app always gets its own.
 */
export function chatSession(chatId: number, deps: ChatSessionDeps = appDeps): ChatSession {
  let s = sessions.get(chatId);
  if (!s) {
    s = create(chatId, deps);
    sessions.set(chatId, s);
  }
  return s;
}

/** Forget a chat entirely (chat closed/deleted), listeners included. */
export function dropChatSession(chatId: number): void {
  const s = sessions.get(chatId);
  if (!s) return;
  s.detach();
  sessions.delete(chatId);
}

/** Chats whose stream is still attached — for debugging and tests. */
export function liveChatSessionIds(): number[] {
  return [...sessions.keys()];
}

// ── reducers ──────────────────────────────────────────────────────────────
// Everything a stream line can change, in one place. Ported from AgentChat.vue,
// where these ran as closures over whichever instance had mounted last.

/** The projection mutates a plain {messages, nextMsgId}; this adapter keeps
 *  lib/chatProjection.ts free of Vue. */
function projectionOf(s: InternalSession): ChatProjectionState {
  return {
    get messages() { return s.messages.value; },
    get nextMsgId() { return s.nextMsgId; },
    set nextMsgId(v: number) { s.nextMsgId = v; },
  };
}

/** Is the user looking at this chat right now — a mounted view and a focused window. */
function watchingNow(s: InternalSession): boolean {
  return s.refCount > 0 && hasFocus();
}

function rememberContext(s: InternalSession) {
  const all = readCtxStore();
  all[String(s.chatId)] = { tokens: s.contextTokens.value, window: s.contextWindow.value, split: s.contextSplit.value };
  try { localStorage.setItem(CTX_STORE_KEY, JSON.stringify(all)); } catch { /* quota */ }
}

/**
 * A turn does not always start with a user send: Claude resumes the same
 * session on its own after a background task finishes or an interim Stop.
 * Assistant output while we think it's idle IS a turn.
 * ponytail: native transport only — ACP replays history through the same feed
 * on session/load with no turn-done, so marking active there sticks running.
 */
function markActive(s: InternalSession) {
  if (s.busy.value) return;
  s.busy.value = true;
  s.sync();
}

/** Everything a finished turn does beyond the transcript. Shared by the native
 *  boundary (turn.completed) and the ACP one (the response to our own
 *  session/prompt, which only the sender can correlate). */
function finishTurn(s: InternalSession) {
  s.busy.value = false;
  s.settleTranscript();
  s.save();
  s.sync();
  s.view?.scrollToBottom();
  // An `exit` from an intentional restart (mode switch / abort) is not a real
  // turn boundary — skip the "finished" toast/notification once.
  if (s.suppressNextDone.value) {
    s.suppressNextDone.value = false;
  } else {
    s.deps.host.notifyDone(s.chatId, watchingNow(s));
  }
  // Only worth keeping now if someone is still watching.
  s.maybeEvict();
}

function onEvents(s: InternalSession, batch: ChatEventBatch) {
  const { host } = s.deps;
  for (const event of batch.events) {
    // Our own prompt coming back. Consumed before the projection sees it, so
    // it neither duplicates the bubble nor counts as agent activity.
    if (event.type === "user.delta" && s.pendingSends.delete(event.text ?? "")) continue;
    // The /context report the card asked for, lifted out of the stream before
    // the projection can turn it into a wall of a message.
    if (s.awaitingContextReport.value && event.type === "text.delta") {
      const rows = parseContextReport(event.text ?? "");
      if (rows) {
        s.contextReport.value = rows;
        s.awaitingContextReport.value = false;
        s.contextReportPending.value = false;
        continue;
      }
    }
    if (isProjectedEvent(event.type)) {
      if (!(s.view?.usesRpcRuntime() ?? false)) markActive(s);
      if (applyChatEvent(projectionOf(s), event)) s.view?.scrollToBottom();
      if (event.type === "tool.started" && event.toolCallId && (event.name === "Task" || event.name === "Agent")) {
        host.subagentStarted(s.chatId, event.toolCallId, event.input);
      }
      if (event.type === "tool.completed" && event.toolCallId) {
        host.subagentCompleted(event.toolCallId, event.failed === true);
      }
      continue;
    }

    switch (event.type) {
      case "turn.completed":
      case "turn.failed":
        s.awaitingContextReport.value = false;
        s.contextReportPending.value = false;
        if (event.type === "turn.completed" && (event.inputTokens || event.outputTokens || event.contextWindow)) {
          const inp = event.inputTokens ?? 0;
          const out = event.outputTokens ?? 0;
          s.turnStats.value = { inputTokens: inp, outputTokens: out, costUsd: event.costUsd ?? 0 };
          if (event.contextWindow) {
            s.contextWindow.value = event.contextWindow;
            rememberContext(s);
          }
          s.sessionCost.value += event.costUsd ?? 0;
          host.recordTurn(inp, out);
        }
        finishTurn(s);
        break;
      case "context.usage":
        if (event.contextTokens) {
          s.contextTokens.value = event.contextTokens;
          // What the prompt cache served, what had to be sent or written to it
          // this turn, and what the model produced. The three add up to the ring.
          s.contextSplit.value = {
            cached: event.cacheReadTokens ?? 0,
            fresh: (event.inputTokens ?? 0) + (event.cacheCreationTokens ?? 0),
            output: event.outputTokens ?? 0,
          };
          rememberContext(s);
        }
        break;
      case "session.title":
        // Once Claude has named the thread, a later result repeating the title
        // must not re-sync it — the user may have renamed the tab since.
        if (!s.claudeGeneratedTitle.value && typeof event.title === "string" && event.title.trim()) {
          s.claudeGeneratedTitle.value = true;
          host.syncChat(s.chatId, { title: event.title.trim().slice(0, 60) });
        }
        break;
      case "session.exited":
        // The process is gone, so the next send must spawn a replacement.
        s.runtimeStarted.value = false;
        if (s.busy.value) {
          // Died mid-turn with no boundary of its own — settle it here or the
          // spinner runs forever.
          s.busy.value = false;
          s.settleTranscript(true);
          s.sync();
        }
        s.maybeEvict();
        break;
    }
  }
}

function hasActiveControlRequest(s: InternalSession, requestId: string) {
  return [s.pendingPermission.value, s.pendingDiff.value, s.pendingQuestion.value, s.pendingPlan.value]
    .some((request) => request?.requestId === requestId);
}

/** Claude withdrew a pending request (turn aborted, answered elsewhere…). */
function dismissCancelledControlRequest(s: InternalSession, requestId: string) {
  const slots = [
    [s.pendingPermission, s.pendingPermissionMsgId],
    [s.pendingDiff, s.pendingDiffMsgId],
    [s.pendingQuestion, s.pendingQuestionMsgId],
    [s.pendingPlan, s.pendingPlanMsgId],
  ] as const;
  let dismissed = false;
  for (const [request, markerId] of slots) {
    if (request.value?.requestId !== requestId) continue;
    s.removeFeedMarker(markerId.value);
    markerId.value = null;
    request.value = null;
    dismissed = true;
  }
  if (dismissed) {
    s.settleControlRequest(requestId);
    s.nativeControlResponsePending.value = false;
    s.sync();
  }
}

const DIFF_TOOLS = ["Edit", "Write", "MultiEdit", "NotebookEdit"];

/** Raw native lines — only what has no domain event: the control (permission)
 *  protocol and the CLI's own bookkeeping. */
function onLine(s: InternalSession, line: string) {
  let event: Record<string, unknown>;
  try { event = JSON.parse(line) as Record<string, unknown>; }
  catch { return; }
  s.lastActivityAt.value = Date.now();
  const type = event.type as string;

  if (type === "control_cancel_request") {
    dismissCancelledControlRequest(s, event.request_id as string);
    return;
  }

  if (type === "control_request") {
    const req = (event.request ?? {}) as Record<string, unknown>;
    if (req.subtype !== "can_use_tool") return; // other control subtypes: ignore (fail-open)
    const cr: CanUseToolReq = {
      requestId: event.request_id as string,
      toolName: (req.tool_name as string) ?? "",
      input: (req.input ?? {}) as Record<string, unknown>,
      description: req.description as string | undefined,
      suggestions: (req.permission_suggestions ?? []) as Array<Record<string, unknown>>,
      toolUseId: req.tool_use_id as string | undefined,
    };
    // A request can be replayed during reconnect. Rendering it again after we
    // replied is what made AskUserQuestion look permanently stuck.
    if (s.settledControlRequestIds.has(cr.requestId) || hasActiveControlRequest(s, cr.requestId)) return;
    // Auto-allow when an "always" rule matches — no UI.
    if (s.deps.host.hasPermissionRule(ruleKeys(cr.toolName, cr.input))) {
      void s.respondControl(cr.requestId, { behavior: "allow", updatedInput: cr.input }).catch((e) => {
        s.messages.value.push({ id: s.nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
        s.save();
      });
      return;
    }
    const marker = (text: string) => {
      const id = s.nextMsgId++;
      s.messages.value.push({ id, role: "system-info", text });
      return id;
    };
    if (cr.toolName === "AskUserQuestion") {
      s.view?.onQuestionOpened();
      s.pendingQuestion.value = cr;
      const qText = ((cr.input.questions as Array<{ question: string }>)?.[0]?.question ?? "Question").slice(0, 80);
      s.pendingQuestionMsgId.value = marker(`❓ ${qText}`);
    } else if (cr.toolName === "ExitPlanMode") {
      s.view?.onPlanOpened();
      s.pendingPlan.value = cr;
      s.pendingPlanMsgId.value = marker(`📋 Plan ready for review`);
    } else if (DIFF_TOOLS.includes(cr.toolName)) {
      s.pendingDiff.value = cr;
      const filePath = ((cr.input.file_path ?? cr.input.path ?? "") as string);
      s.pendingDiffMsgId.value = marker(`✏️ ${cr.toolName}: ${filePath.split("/").slice(-2).join("/")}`);
    } else {
      s.pendingPermission.value = cr;
      s.pendingPermissionMsgId.value = marker(`⚡ ${cr.toolName} wants permission`);
    }
    s.deps.host.notifyPermission(s.chatId, cr);
    s.sync();
    s.view?.scrollToBottom();
    return;
  }

  if (type === "system" && event.subtype === "init") {
    const sid = (event.session_id as string) ?? "";
    s.sessionId.value = sid;
    s.deps.host.syncChat(s.chatId, { claudeSessionId: sid });
  }
  // assistant / user (tool results) / result / exit arrive as domain events.
}

/** Lines from acp-data-{chatId}: the session handshake, selector replies, the
 *  session/prompt response that ends a turn, Codex's serverRequest/resolved. */
function onAcpData(s: InternalSession, raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { console.warn(`[chat-diag] unparseable acp-data line, dropped (len=${raw.length})`); return; }
  s.lastActivityAt.value = Date.now();

  // Codex resolves requests asynchronously. The approval stays visible until
  // this acknowledgement, so a failed response can be retried instead of
  // looking like an automatic deny or a lost prompt.
  if (msg.method === "serverRequest/resolved") {
    const requestId = (msg.params as { requestId?: number })?.requestId;
    if (requestId != null && requestId === s.acpPermRpcId.value) {
      s.removeFeedMarker(s.acpPermMsgId.value); s.acpPermMsgId.value = null;
      s.acpPermReq.value = null;
      s.acpPermRpcId.value = null;
      s.permissionResponsePending.value = false;
      s.sync();
    }
    return;
  }

  // Emitted by acp_start after the handshake: sessionId (for resume) plus the
  // modes/configOptions that populate the selectors.
  if (msg._burrow === "session") {
    const sid = msg.sessionId as string;
    if (sid) { s.sessionId.value = sid; s.deps.host.syncChat(s.chatId, { claudeSessionId: sid }); }
    s.acpModes.value = (msg.modes as AcpModes) ?? null;
    s.acpConfigOptions.value = (msg.configOptions as AcpConfigOption[]) ?? [];
    s.view?.onAcpSession();
    // Finalize messages rendered from a session/load replay (no turn-done
    // fires for a load) and persist the restored history.
    if (s.messages.value.some((m) => m.partial)) {
      s.settleTranscript();
      s.save();
      s.view?.scrollToBottom();
    }
    s.view?.restoreAcpSelections();
    return;
  }

  if ("id" in msg && !("method" in msg)) {
    const rid = msg.id as number;
    if (s.acpControlIds.has(rid)) {
      s.acpControlIds.delete(rid);
      // Reply to a restore push we sent ourselves: apply it, but do NOT
      // restore from it — that is the ping-pong the guard exists for.
      const wasRestorePush = s.acpRestorePushIds.delete(rid);
      const result = msg.result as { configOptions?: AcpConfigOption[]; modes?: AcpModes } | undefined;
      if (result?.configOptions) s.acpConfigOptions.value = result.configOptions;
      if (result?.modes) s.acpModes.value = result.modes;
      // A model / mode / effort switch comes back with the adapter's whole
      // selector set reset to its defaults — put the user's picks back.
      if (!wasRestorePush && (result?.configOptions || result?.modes)) s.view?.restoreAcpSelections();
      return;
    }
    // The turn is settled by the response to OUR session/prompt.
    if (s.acpPromptRpcId.value === null || rid !== s.acpPromptRpcId.value) return;
    s.acpPromptRpcId.value = null;
    finishTurn(s);
  }
  // session/update and the {_burrow:"exit"} EOF arrive as domain events.
}

/** Lines from acp-req-{chatId}: blocking permission and user-input requests. */
function onAcpReq(s: InternalSession, raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { return; }
  if (msg.method === "item/tool/requestUserInput") {
    const params = (msg.params ?? {}) as Record<string, unknown>;
    const questions = ((params.questions ?? []) as Array<Record<string, unknown>>)
      .filter((question) => typeof question.id === "string" && typeof question.question === "string")
      .map((question) => ({
        id: question.id as string,
        header: typeof question.header === "string" ? question.header : "Question",
        question: question.question as string,
        isOther: question.isOther === true,
        isSecret: question.isSecret === true,
        options: ((question.options ?? []) as Array<Record<string, unknown>>)
          .filter((option) => typeof option.label === "string")
          .map((option) => ({ label: option.label as string, ...(typeof option.description === "string" ? { description: option.description } : {}) })),
      }));
    if (typeof msg.id !== "number" || questions.length === 0) return;
    s.codexUserInput.value = { rpcId: msg.id, questions };
    s.codexUserInputPending.value = false;
    s.sync();
    return;
  }
  const perm = parseAcpPermRequest(msg);
  if (!perm) return;

  s.acpPermRpcId.value = perm.rpcId;
  // Render the adapter's OWN option list — don't flatten to Y/N.
  s.acpPermReq.value = {
    rpcId: perm.rpcId,
    toolCallId: perm.toolCallId,
    title: perm.title,
    kind: perm.kind,
    options: perm.options,
    rawInput: perm.rawInput,
  };
  const isPlan = typeof perm.rawInput?.plan === "string";
  const id = s.nextMsgId++;
  s.acpPermMsgId.value = id;
  s.messages.value.push({ id, role: "system-info", text: isPlan ? "📋 Plan ready for review" : `⚡ Permission: ${perm.title}` });
  s.deps.host.notifyPermission(s.chatId, { requestId: String(perm.rpcId), toolName: perm.title, input: perm.rawInput, suggestions: [] } as CanUseToolReq);
  s.sync();
  s.view?.scrollToBottom();
}
