import { ref, type Ref } from "vue";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { invoke } from "@tauri-apps/api/core";
import type {
  AcpConfigOption, AcpModes, AcpPermReq, CanUseToolReq, ChatMessage, QueuedChatMessage, TurnStats,
} from "@/lib/chatTypes";
import type { ChatEventBatch } from "@/lib/chatProjection";

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
// ponytail: this holds only the state that must survive an unmount. Everything
// tied to the DOM (scroll element, menus, draft text, the copy-feedback timer)
// deliberately stays in the component, where it is recreated per mount — moving
// it here would just be a bigger diff with a longer-lived leak.

/** Handlers a mounted view installs; the session calls whichever is current. */
export interface ChatStreamHandlers {
  /** Provider-neutral events — the transcript and the turn boundary. */
  onEvents: (batch: ChatEventBatch) => void;
  /** Raw native lines — only what has no domain event yet (permissions). */
  onLine: (line: string) => void;
  /** Raw ACP lines — session handshake, selector replies, rpc correlation. */
  onAcpData: (line: string) => void;
  onAcpReq: (line: string) => void;
}

const NOOP_HANDLERS: ChatStreamHandlers = {
  onEvents: () => {}, onLine: () => {}, onAcpData: () => {}, onAcpReq: () => {},
};

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
  /** FIFO of follow-ups. Never send these to a provider while a turn is active. */
  messageQueue: Ref<QueuedChatMessage[]>;
  enqueueMessage(text: string, images?: string[]): QueuedChatMessage;
  removeQueuedMessage(id: number): void;
  clearQueuedMessages(): void;
  moveQueuedMessageNext(id: number): void;
  takeNextQueuedMessage(): QueuedChatMessage | undefined;
  restoreQueuedMessages(): void;
  suppressNextDone: Ref<boolean>;
  sessionId: Ref<string>;
  turnStats: Ref<TurnStats | null>;
  sessionCost: Ref<number>;
  runtimeStarted: Ref<boolean>;

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

  // ── blocking requests (ACP transport) ──
  acpPermReq: Ref<AcpPermReq | null>;
  /** JSON-RPC id of the pending request. Lives here, not in the view: the
   *  `serverRequest/resolved` that clears the prompt can arrive after a
   *  remount, and a fresh view's local id would never match it. */
  acpPermRpcId: Ref<number | null>;
  acpPermMsgId: Ref<number | null>;
  acpPromptRpcId: Ref<number | null>;
  acpControlIds: Set<number>;
  acpModes: Ref<AcpModes | null>;
  acpConfigOptions: Ref<AcpConfigOption[]>;

  /** Install the current view's reducers. Replaces any previous view's. */
  setHandlers(h: Partial<ChatStreamHandlers>): void;
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
  handlers: ChatStreamHandlers;
  refCount: number;
  evictTimer: ReturnType<typeof setTimeout> | null;
  eventsUL: UnlistenFn | null;
  claudeUL: UnlistenFn | null;
  acpDataUL: UnlistenFn | null;
  acpReqUL: UnlistenFn | null;
}

const sessions = new Map<number, InternalSession>();

/** What the Go side puts on `claude-data-*` / `acp-data-*` / `acp-req-*`. */
export interface StreamEvent { ord: number; kind: string; line: string }

/**
 * Record the ord, then hand the reducer the bare line — so the whole component
 * stays unaware that the stream is numbered, and only the session has to know.
 */
type RawHandlerKey = "onLine" | "onAcpData" | "onAcpReq";

function feed(s: InternalSession, ev: StreamEvent, key: RawHandlerKey) {
  if (ev.ord > s.lastOrd) s.lastOrd = ev.ord;
  s.handlers[key](ev.line);
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
    const folded = await invoke<number>("chat_folded_ord", { chatId });
    if (!folded) return;
    // Domain events, not raw lines: a replay should rebuild the transcript and
    // nothing else. Re-feeding raw would also re-open permission requests that
    // were answered before the restart.
    const batches = await invoke<ChatEventBatch[]>("load_chat_events_since", { chatId, since: folded });
    for (const batch of batches) {
      if (batch.ord > s.lastOrd) s.lastOrd = batch.ord;
      s.handlers.onEvents(batch);
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
  return !s.busy.value
    && s.pendingPermission.value === null
    && s.pendingQuestion.value === null
    && s.pendingPlan.value === null
    && s.pendingDiff.value === null
    && s.acpPermReq.value === null;
}

function create(chatId: number): InternalSession {
  const s: InternalSession = {
    chatId,
    messages: ref<ChatMessage[]>([]),
    nextMsgId: 0,
    lastOrd: -1,
    replayed: false,

    busy: ref(false),
    lastActivityAt: ref(Date.now()),
    turnStartedAt: ref(0),
    messageQueue: ref<QueuedChatMessage[]>([]),
    suppressNextDone: ref(false),
    sessionId: ref(""),
    turnStats: ref<TurnStats | null>(null),
    sessionCost: ref(0),
    runtimeStarted: ref(false),

    pendingPermission: ref<CanUseToolReq | null>(null),
    pendingQuestion: ref<CanUseToolReq | null>(null),
    pendingPlan: ref<CanUseToolReq | null>(null),
    pendingDiff: ref<CanUseToolReq | null>(null),
    pendingPermissionMsgId: ref<number | null>(null),
    pendingQuestionMsgId: ref<number | null>(null),
    pendingPlanMsgId: ref<number | null>(null),
    pendingDiffMsgId: ref<number | null>(null),
    settledControlRequestIds: new Set<string>(),

    acpPermReq: ref<AcpPermReq | null>(null),
    acpPermRpcId: ref<number | null>(null),
    acpPermMsgId: ref<number | null>(null),
    acpPromptRpcId: ref<number | null>(null),
    acpControlIds: new Set<number>(),
    acpModes: ref<AcpModes | null>(null),
    acpConfigOptions: ref<AcpConfigOption[]>([]),

    enqueueMessage(text, images) {
      const entry: QueuedChatMessage = { id: s.nextMsgId++, text, ...(images?.length ? { images } : {}) };
      s.messageQueue.value.push(entry);
      s.messages.value.push({ id: entry.id, role: "queued", text, ...(entry.images ? { images: entry.images } : {}) });
      return entry;
    },
    removeQueuedMessage(id) {
      s.messageQueue.value = s.messageQueue.value.filter((entry) => entry.id !== id);
      s.messages.value = s.messages.value.filter((message) => message.id !== id || message.role !== "queued");
    },
    clearQueuedMessages() {
      s.messageQueue.value = [];
      s.messages.value = s.messages.value.filter((message) => message.role !== "queued");
    },
    moveQueuedMessageNext(id) {
      const index = s.messageQueue.value.findIndex((entry) => entry.id === id);
      if (index <= 0) return;
      const [entry] = s.messageQueue.value.splice(index, 1);
      s.messageQueue.value.unshift(entry);
    },
    takeNextQueuedMessage() {
      const entry = s.messageQueue.value.shift();
      if (entry) s.messages.value = s.messages.value.filter((message) => message.id !== entry.id || message.role !== "queued");
      return entry;
    },
    restoreQueuedMessages() {
      if (s.messageQueue.value.length > 0) return;
      s.messageQueue.value = s.messages.value
        .filter((message) => message.role === "queued")
        .map((message) => ({ id: message.id, text: message.text, ...(message.images?.length ? { images: message.images } : {}) }));
    },

    handlers: { ...NOOP_HANDLERS },
    refCount: 0,
    evictTimer: null,
    eventsUL: null,
    claudeUL: null,
    acpDataUL: null,
    acpReqUL: null,

    setHandlers(h) {
      s.handlers = { ...s.handlers, ...h };
    },
    async listenEvents() {
      if (!s.eventsUL) {
        s.eventsUL = await listen<ChatEventBatch>(`chat-event-${chatId}`, (e) => {
          if (e.payload.ord > s.lastOrd) s.lastOrd = e.payload.ord;
          s.handlers.onEvents(e.payload);
        });
      }
    },
    async listenClaude() {
      // The listener closes over `s.handlers`, not over the handler that was
      // current at attach time — so a remount swaps the reducer without ever
      // re-subscribing, and the gap that used to lose lines never opens.
      if (!s.claudeUL) {
        s.claudeUL = await listen<StreamEvent>(`claude-data-${chatId}`, (e) => feed(s, e.payload, "onLine"));
      }
    },
    async listenAcp() {
      if (!s.acpDataUL) {
        s.acpDataUL = await listen<StreamEvent>(`acp-data-${chatId}`, (e) => feed(s, e.payload, "onAcpData"));
      }
      if (!s.acpReqUL) {
        s.acpReqUL = await listen<StreamEvent>(`acp-req-${chatId}`, (e) => feed(s, e.payload, "onAcpReq"));
      }
    },
    detach() {
      if (s.evictTimer) { clearTimeout(s.evictTimer); s.evictTimer = null; }
      s.eventsUL?.(); s.eventsUL = null;
      s.claudeUL?.(); s.claudeUL = null;
      s.acpDataUL?.(); s.acpDataUL = null;
      s.acpReqUL?.(); s.acpReqUL = null;
      s.handlers = { ...NOOP_HANDLERS };
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
  return s;
}

/** The session for a chat, created on first use. Does not retain. */
export function chatSession(chatId: number): ChatSession {
  let s = sessions.get(chatId);
  if (!s) {
    s = create(chatId);
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
