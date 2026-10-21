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
import { isImageStub, parseImageStub } from "@/lib/chatImages";
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

/** One thing the agent is blocked on, whatever the transport. */
export type PendingRequest =
  | { kind: "permission"; request: CanUseToolReq }
  | { kind: "diff"; request: CanUseToolReq }
  | { kind: "question"; request: CanUseToolReq }
  | { kind: "plan"; request: CanUseToolReq }
  | { kind: "acpPermission"; request: AcpPermReq }
  | { kind: "codexInput"; request: CodexUserInputReq };

/** The user's answer to the matching pending request. */
export type PendingAnswer =
  /** A native tool permission or file diff (Allow / Always / Deny). */
  | { kind: "permission"; allow: boolean; always?: boolean; updatedInput?: Record<string, unknown>; message?: string }
  /** AskUserQuestion — answers keyed by question text; {} dismisses it. */
  | { kind: "question"; answers: Record<string, string | string[]> }
  /** ExitPlanMode — approve, or keep planning with feedback. */
  | { kind: "plan"; approve: boolean; feedback?: string }
  /** An ACP / Codex permission — one of the adapter's own options. */
  | { kind: "acpOption"; optionId: string; label: string; reject: boolean }
  /** Codex requestUserInput — answers keyed by question id; all-empty dismisses it. */
  | { kind: "codexInput"; answers: Record<string, string[]> };

// Remembered per chat, because replayChatStream only replays from folded_ord —
// the usage that filled the context ring is usually behind that mark, so
// nothing would re-emit it and the ring would vanish until the next turn.
// Stale after an external /clear, and self-healing on the next turn.
// ponytail: one unpruned map; revisit if someone keeps thousands of chats.
/** Messages per window when a transcript is read back (`load_chat_messages_page`). */
export const TRANSCRIPT_PAGE = 60;
/** One window of a transcript: rows [start, start+messages.length) of `total`. */
export interface ChatMessagesPage {
  messages: ChatMessage[];
  start: number;
  total: number;
}

const CTX_STORE_KEY = "burrow.ctxTokens";
type CtxEntry = { tokens: number; window: number; split?: CtxSplit | null };
function readCtxStore(): Record<string, CtxEntry> {
  try { return JSON.parse(localStorage.getItem(CTX_STORE_KEY) || "{}"); } catch { return {}; }
}

function hasFocus(): boolean {
  return typeof document !== "undefined" && document.hasFocus();
}

/**
 * The session's state. Writable here, inside the module, where the reducers
 * and the intent methods below keep its invariants; everything outside reads
 * it through ChatSession, which hands the same refs back read-only.
 */
interface SessionState {
  // ── transcript ──
  messages: Ref<ChatMessage[]>;
  /**
   * How many EARLIER messages exist in storage that `messages` does not hold
   * (a long chat is opened from its tail, `loadEarlier` fetches the rest).
   * While it is > 0, `messages` is a window, so nothing may write it back as
   * the whole transcript — see `save`.
   */
  hiddenBefore: Ref<number>;
  loadingEarlier: Ref<boolean>;

  // ── turn state ──
  busy: Ref<boolean>;
  lastActivityAt: Ref<number>;
  /** Set while busy; stamped onto the turn's user message as turnMs when it settles. */
  turnStartedAt: Ref<number>;
  sessionId: Ref<string>;
  turnStats: Ref<TurnStats | null>;
  sessionCost: Ref<number>;

  // ── context meter + /context report ──
  contextTokens: Ref<number>;
  contextWindow: Ref<number>;
  contextSplit: Ref<CtxSplit | null>;
  contextReport: Ref<CtxReportRow[] | null>;
  contextReportPending: Ref<boolean>;
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
  /** A native control response is being written; the prompt stays until it lands. */
  nativeControlResponsePending: Ref<boolean>;

  // ── blocking requests (ACP transport) ──
  acpPermReq: Ref<AcpPermReq | null>;
  /** JSON-RPC id of the pending request. Lives here, not in the view: the
   *  `serverRequest/resolved` that clears the prompt can arrive after a
   *  remount, and a fresh view's local id would never match it. */
  acpPermRpcId: Ref<number | null>;
  acpPermMsgId: Ref<number | null>;
  /** An answer to acpPermReq is in flight (Codex clears it on serverRequest/resolved). */
  permissionResponsePending: Ref<boolean>;
  /** Codex `item/tool/requestUserInput`: blocking, so it must outlive the view. */
  codexUserInput: Ref<CodexUserInputReq | null>;
  codexUserInputPending: Ref<boolean>;
  acpModes: Ref<AcpModes | null>;
  acpConfigOptions: Ref<AcpConfigOption[]>;
}

type ReadonlyRefs<T> = { readonly [K in keyof T]: T[K] extends Ref<infer V> ? Readonly<Ref<V>> : T[K] };

/** A user turn as the composer hands it over. */
export interface UserPrompt {
  text: string;
  images?: string[];
  /** What the bubble shows when it differs from what was sent (paste chips). */
  displayText?: string;
}

/**
 * A chat's live session, as everything outside this module sees it: read-only
 * state plus intents. Nothing outside writes a ref — that is what keeps the
 * idle invariant (no pending request, not busy → evictable) true, since the
 * only code that can break it is in this file.
 */
export interface ChatSession extends ReadonlyRefs<Omit<SessionState, "messages">> {
  readonly chatId: number;
  /** Read-only: rows are added by the reducers and the intents below. */
  readonly messages: Readonly<Ref<readonly ChatMessage[]>>;

  /**
   * FIFO of follow-ups, DERIVED from the transcript's own `queued` entries.
   * There is deliberately no second array: the queue and its placeholder
   * bubbles used to be two structures synchronised by hand in five methods,
   * and `moveQueuedMessageNext` (now `moveQueuedMessage`) only ever updated one of them — "Send Next"
   * reordered what was sent while the transcript kept showing the old order.
   * Deriving it also makes the queue survive a relaunch for free, since the
   * placeholders are persisted with the transcript.
   */
  messageQueue: ComputedRef<QueuedChatMessage[]>;
  enqueueMessage(text: string, images?: string[]): QueuedChatMessage;
  removeQueuedMessage(id: number): void;
  clearQueuedMessages(): void;
  /** Move a queued follow-up to position `toIndex` of the queue (0 = next). */
  moveQueuedMessage(id: number, toIndex: number): void;

  /** Every request the agent is blocked on, whatever the transport. */
  pendingRequests: ComputedRef<PendingRequest[]>;
  /**
   * Answer the matching pending request — the one door for every transport.
   * Resolves true when the agent accepted the answer. A failed write leaves an
   * error row in the transcript; whether the prompt stays depends on the
   * protocol (a native prompt is dropped, an ACP one stays retryable).
   */
  respond(answer: PendingAnswer): Promise<boolean>;

  // ── intents ──
  /** An app-authored assistant row (a failed action, an error) — saved with the transcript. */
  notice(text: string): void;
  /** Show the user's prompt and claim its echo. Returns the bubble's id. Does not start the turn. */
  postUserMessage(prompt: UserPrompt): number;
  /** The prompt is on its way: the chat is busy until a turn boundary. */
  startTurn(): void;
  /** The send failed: report it and leave the turn. */
  failTurn(error: unknown): void;
  /**
   * Send a prompt as its own turn — the one door for every transport: show
   * the bubble, mark busy, `chat_send` (the Go chat runtime starts the process
   * if there is none). Resolves the bubble's id, or null when the send failed
   * (already reported in the transcript).
   */
  send(prompt: UserPrompt): Promise<number | null>;
  /** Stop the process; Go publishes session.reset and the next send starts a fresh one. */
  restart(): Promise<void>;
  /** End the running turn (no "finished" toast); Go restarts if the provider can't stop in place. */
  interrupt(): Promise<void>;
  /** Wipe the transcript and the agent session; Go publishes session.reset{cleared}. */
  clear(): Promise<void>;
  /**
   * Drop this user message and everything after it; the agent's next turn
   * continues from just before it (Claude only, `chat_rewind`). Resolves false
   * when Go refused (already reported).
   */
  rewind(message: ChatMessage): Promise<boolean>;
  /** A new chat continuing from this message's turn (`chat_fork`). Resolves its id, or null. */
  fork(message: ChatMessage): Promise<number | null>;
  /** A selector push we sent; `restore` marks our own restore push, whose reply must not restore again. */
  trackAcpControl(rpcId: number, restore?: boolean): void;
  /** Optimistically show an ACP selector pick ("mode", or a config option id) before the adapter confirms. */
  showAcpSelection(id: string, value: string): void;
  /** Learned or cached ACP capabilities, for a session that has none yet. */
  seedAcpCapabilities(modes: AcpModes | null, configOptions: AcpConfigOption[]): void;
  /** The coming turn end is ours (restart, interrupt) — no "finished" toast. */
  suppressDoneToast(): void;
  /** Ask the CLI for `/context` out of band; its report is lifted out of the stream. */
  requestContextReport(): Promise<void>;
  /** First paint from storage, for a session that has no transcript yet. */
  hydrate(state: { messages?: ChatMessage[]; hiddenBefore?: number; sessionId?: string }): void;
  /** Fetch the next page of older messages into the window (`load_chat_messages_page`). */
  loadEarlier(): Promise<void>;
  /** Resolve image `i` of a message that arrived as a stub (`load_chat_image`); returns its data URL. */
  loadImage(message: ChatMessage, i: number): Promise<string>;
  /** Take the last user message back out to edit it (↑ in an empty composer). */
  recallLastUserMessage(): string | null;

  // ── lifecycle ──
  /** Point the session's hooks at this view. Replaces any previous view's. */
  attachView(view: ChatViewHooks): void;
  /** Persist the transcript (fire-and-forget), with the folded stream mark. */
  save(): void;
  /** Mirror busy / message count into the chat list and the remote mirror. */
  sync(): void;
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

interface InternalSession extends SessionState, Omit<ChatSession, keyof SessionState> {
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
  suppressNextDone: Ref<boolean>;
  /** A `/context` was sent out of band; its report is lifted out of the stream. */
  awaitingContextReport: Ref<boolean>;
  settledControlRequestIds: Set<string>;
  acpControlIds: Set<number>;
  /** Ids of OUR OWN selector restore pushes — their replies must not restore again. */
  acpRestorePushIds: Set<number>;
  takeNextQueuedMessage(): QueuedChatMessage | undefined;
  /** A prompt this client is about to send — drop its echo when Go publishes it. */
  expectEcho(text: string): void;
  /** Settle partial rows and stuck tool spinners (a turn ended somehow). */
  settleTranscript(failedTools?: boolean): void;
  /** Remove a system-info marker row by id. */
  removeFeedMarker(id: number | null): void;
  /** Record a native control request as answered (a replay must not reopen it). */
  settleControlRequest(requestId: string): void;
  /** Answer a native can_use_tool request. Throws if the write failed. */
  respondControl(requestId: string, response: Record<string, unknown>): Promise<void>;
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
    hiddenBefore: ref(0),
    loadingEarlier: ref(false),
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
    permissionResponsePending: ref(false),
    codexUserInput: ref<CodexUserInputReq | null>(null),
    codexUserInputPending: ref(false),
    acpControlIds: new Set<number>(),
    acpRestorePushIds: new Set<number>(),
    acpModes: ref<AcpModes | null>(null),
    acpConfigOptions: ref<AcpConfigOption[]>([]),

    pendingRequests: computed(() => {
      const out: PendingRequest[] = [];
      if (s.pendingPermission.value) out.push({ kind: "permission", request: s.pendingPermission.value });
      if (s.pendingDiff.value) out.push({ kind: "diff", request: s.pendingDiff.value });
      if (s.pendingQuestion.value) out.push({ kind: "question", request: s.pendingQuestion.value });
      if (s.pendingPlan.value) out.push({ kind: "plan", request: s.pendingPlan.value });
      if (s.acpPermReq.value) out.push({ kind: "acpPermission", request: s.acpPermReq.value });
      if (s.codexUserInput.value) out.push({ kind: "codexInput", request: s.codexUserInput.value });
      return out;
    }),
    respond: (answer) => respond(s, answer),

    notice(text) {
      pushRow(s, "assistant", text);
    },
    postUserMessage({ text, images, displayText }) {
      const id = s.nextMsgId++;
      s.messages.value.push({ id, role: "user", text, ...(images?.length ? { images } : {}), ...(displayText ? { displayText } : {}) });
      // Claim the echo before the send goes out — a loopback round trip can
      // land user.delta before the caller's next line runs.
      s.expectEcho(text);
      return id;
    },
    startTurn() {
      s.busy.value = true;
    },
    failTurn(error) {
      pushRow(s, "assistant", `Error: ${error}`);
      s.busy.value = false;
      s.sync();
    },
    async send(prompt) {
      const id = s.postUserMessage(prompt);
      s.startTurn();
      s.save();
      s.sync();
      s.view?.scrollToBottom(true);
      try {
        await s.deps.invoke("chat_send", { chatId, text: prompt.text, images: prompt.images ?? [] });
        return id;
      } catch (e) {
        s.failTurn(e);
        return null;
      }
    },
    async restart() {
      await s.deps.invoke("chat_restart", { chatId }).catch(() => {});
    },
    async interrupt() {
      // The trailing turn end of a stopped turn is not a "finished" one.
      s.suppressNextDone.value = true;
      await s.deps.invoke("chat_interrupt", { chatId }).catch(() => {});
    },
    async clear() {
      // The reset that empties the transcript arrives on the events channel.
      await s.listenEvents();
      await s.deps.invoke("chat_clear", { chatId }).catch((e: unknown) => s.notice(`Could not clear the chat: ${e}`));
    },
    async rewind(message) {
      const before = s.messages.value;
      const index = before.findIndex((m) => m.id === message.id && m.role === message.role);
      if (index < 0) return false;
      // Cut first: a save racing the call must write the truncated transcript,
      // not put the dropped turns back. Queued follow-ups stay queued.
      s.messages.value = [...before.slice(0, index), ...before.slice(index).filter((m) => m.role === "queued")];
      try {
        await s.deps.invoke("chat_rewind", { chatId, index: index + s.hiddenBefore.value, text: message.text });
        return true;
      } catch (e) {
        s.messages.value = before;
        s.notice(`Could not rewind: ${e}`);
        return false;
      }
    },
    async fork(message) {
      const index = s.messages.value.findIndex((m) => m.id === message.id && m.role === message.role);
      if (index < 0) return null;
      try {
        const chat = await s.deps.invoke<{ id: number }>("chat_fork", { chatId, index: index + s.hiddenBefore.value, text: message.text });
        return chat.id;
      } catch (e) {
        s.notice(`Could not fork: ${e}`);
        return null;
      }
    },
    trackAcpControl(rpcId, restore = false) {
      s.acpControlIds.add(rpcId);
      if (restore) s.acpRestorePushIds.add(rpcId);
    },
    showAcpSelection(id, value) {
      if (id === "mode") {
        if (s.acpModes.value) s.acpModes.value.currentModeId = value;
        return;
      }
      const option = s.acpConfigOptions.value.find((o) => o.id === id);
      if (option) option.currentValue = value;
    },
    seedAcpCapabilities(modes, configOptions) {
      if (s.acpModes.value || s.acpConfigOptions.value.length) return;
      s.acpModes.value = modes;
      s.acpConfigOptions.value = configOptions;
    },
    suppressDoneToast() {
      s.suppressNextDone.value = true;
    },
    async requestContextReport() {
      if (s.busy.value || s.contextReportPending.value) return;
      s.contextReportPending.value = true;
      s.awaitingContextReport.value = true;
      try {
        await s.deps.invoke("chat_send", { chatId, text: "/context", images: [] });
      } catch {
        s.contextReportPending.value = false;
        s.awaitingContextReport.value = false;
      }
    },
    async loadImage(message, i) {
      const src = message.images?.[i] ?? "";
      const ref = parseImageStub(src);
      if (!ref) return src;
      const data = await s.deps.invoke<string>("load_chat_image", { chatId, index: ref.index, image: ref.image });
      // The row the view holds is this session's own object — replace the stub
      // in place so every reader (the bubble, ↑-to-edit, a later save) sees the image.
      const row = s.messages.value.find((m) => m.id === message.id && m.role === message.role);
      if (row?.images?.[i] === src) row.images[i] = data;
      return data;
    },
    async loadEarlier() {
      if (s.loadingEarlier.value || s.hiddenBefore.value <= 0) return;
      s.loadingEarlier.value = true;
      try {
        const page = await s.deps.invoke<ChatMessagesPage>("load_chat_messages_page", {
          chatId, before: s.hiddenBefore.value, limit: TRANSCRIPT_PAGE,
        });
        const older = page.messages;
        const have = new Set(s.messages.value.map((m) => `${m.role}:${m.id}`));
        s.messages.value = [...older.filter((m) => !have.has(`${m.role}:${m.id}`)), ...s.messages.value];
        s.hiddenBefore.value = page.start;
      } catch (e) {
        s.notice(`Could not load earlier messages: ${e}`);
      } finally {
        s.loadingEarlier.value = false;
      }
    },
    hydrate({ messages, hiddenBefore, sessionId }) {
      if (messages && s.messages.value.length === 0) {
        s.messages.value = messages;
        s.hiddenBefore.value = hiddenBefore ?? 0;
        // Ids must continue past the loaded history, or new messages collide
        // with old ones on the same `:key`.
        s.nextMsgId = messages.reduce((max, m) => Math.max(max, m.id + 1), s.nextMsgId);
      }
      if (sessionId) s.sessionId.value = sessionId;
    },
    recallLastUserMessage() {
      const lastUser = [...s.messages.value].reverse().find((m) => m.role === "user");
      if (!lastUser) return null;
      s.messages.value = s.messages.value.filter((m) => m !== lastUser);
      return lastUser.text;
    },

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
    moveQueuedMessage(id, toIndex) {
      const messages = s.messages.value;
      const queue = messages.filter((m) => m.role === "queued");
      const from = queue.findIndex((m) => m.id === id);
      const to = Math.min(Math.max(toIndex, 0), queue.length - 1);
      if (from < 0 || from === to) return;
      // Land right before (moving up) or after (moving down) the entry now at `to`.
      const target = queue[to];
      const [entry] = messages.splice(messages.indexOf(queue[from]), 1);
      messages.splice(messages.indexOf(target) + (to > from ? 1 : 0), 0, entry);
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
      // A window is not the transcript: save_chat_messages replaces the whole
      // chat, so writing this would delete everything above it. Go folds and
      // persists the stream itself (chattranscript.go); what this skips is only
      // client-made rows (notes), which are not worth a truncated history.
      if (s.hiddenBefore.value > 0) return;
      // Same for a transcript still holding image stubs: they would replace the stored images.
      if (s.messages.value.some((m) => m.images?.some(isImageStub))) return;
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
        messageCount: s.messages.value.filter((m) => m.role !== "tool").length + s.hiddenBefore.value,
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
  // Turn clock: stamped onto the user message that opened the turn when it
  // settles, so it persists with the history and the fold row can say
  // "Worked for 26s" after a reload.
  scope.run(() => watch(s.busy, (running) => {
    if (running) {
      s.lastActivityAt.value = Date.now();
      s.turnStartedAt.value = Date.now();
      return;
    }
    if (!s.turnStartedAt.value) return;
    const lastUser = [...s.messages.value].reverse().find((m) => m.role === "user");
    if (lastUser) lastUser.turnMs = Date.now() - s.turnStartedAt.value;
    s.turnStartedAt.value = 0;
  }, { flush: "sync" }));
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

/** Everything a finished turn does beyond the transcript. Every transport ends
 *  its turn with turn.completed / turn.failed — for Codex / ACP the Go runtime
 *  emits it after the response to its own prompt. */
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
      case "session.reset":
        // The process is gone — it died (exited) or the chat runtime stopped
        // it on purpose (reset: restart, interrupt, clear, a launch-settings
        // change). Either way the next send spawns a replacement, nothing the
        // old process asked is answerable any more, and no turn is running.
        {
          const cleared = event.type === "session.reset" && event.cleared === true;
          resetRuntime(s, cleared);
          // Go restarted the stream's numbering with it.
          if (cleared) s.lastOrd = batch.ord;
        }
        break;
    }
  }
}

/**
 * The process is gone — nothing it asked is answerable any more and no turn is
 * running. `cleared` also empties the transcript and the turn accounting.
 */
function resetRuntime(s: InternalSession, cleared: boolean) {
  dropPendingRequests(s);
  if (cleared) {
    s.messages.value = [];
    s.hiddenBefore.value = 0;
    s.sessionId.value = "";
    s.turnStats.value = null;
    s.sessionCost.value = 0;
    s.claudeGeneratedTitle.value = false;
  }
  if (s.busy.value) {
    // Died mid-turn with no boundary of its own — settle it here or the
    // spinner runs forever.
    s.busy.value = false;
    s.settleTranscript(true);
  } else {
    s.settleTranscript();
  }
  s.sync();
  s.maybeEvict();
}

/** Every pending request belongs to a process; when it goes, they all go. */
function dropPendingRequests(s: InternalSession) {
  const slots = [
    [s.pendingPermission, s.pendingPermissionMsgId],
    [s.pendingDiff, s.pendingDiffMsgId],
    [s.pendingQuestion, s.pendingQuestionMsgId],
    [s.pendingPlan, s.pendingPlanMsgId],
    [s.acpPermReq, s.acpPermMsgId],
  ] as const;
  for (const [request, markerId] of slots) {
    if (request.value) s.removeFeedMarker(markerId.value);
    markerId.value = null;
    request.value = null;
  }
  s.nativeControlResponsePending.value = false;
  s.acpPermRpcId.value = null;
  s.permissionResponsePending.value = false;
  s.codexUserInput.value = null;
  s.codexUserInputPending.value = false;
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

/** Lines from acp-data-{chatId}: the session handshake, selector replies,
 *  Codex's serverRequest/resolved. The turn end is a domain event. */
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

  // Emitted by the Go runtime after an ACP / Codex handshake: sessionId (for resume) plus the
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
    // A selector reply carries the adapter's whole selector set — apply it
    // whoever asked (the Go runtime pushes the row's settings after a start).
    const result = msg.result as { configOptions?: AcpConfigOption[]; modes?: AcpModes } | undefined;
    if (result?.configOptions) s.acpConfigOptions.value = result.configOptions;
    if (result?.modes) s.acpModes.value = result.modes;
    if (!s.acpControlIds.delete(rid)) return;
    // Reply to a restore push we sent ourselves: apply it, but do NOT
    // restore from it — that is the ping-pong the guard exists for.
    const wasRestorePush = s.acpRestorePushIds.delete(rid);
    // A model / mode / effort switch comes back with the adapter's whole
    // selector set reset to its defaults — put the user's picks back.
    if (!wasRestorePush && (result?.configOptions || result?.modes)) s.view?.restoreAcpSelections();
  }
  // session/update, the turn boundary and the {_burrow:"exit"} EOF arrive as
  // domain events.
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

// ── answering ─────────────────────────────────────────────────────────────

function pushRow(s: InternalSession, role: ChatMessage["role"], text: string) {
  s.messages.value.push({ id: s.nextMsgId++, role, text });
  s.save();
}

/**
 * Write a native control response and drop the prompt it answered. The prompt
 * goes either way: a failed write is reported in the transcript rather than
 * left on screen as a prompt nothing is listening to.
 */
async function resolveNative(
  s: InternalSession,
  requestId: string,
  response: Record<string, unknown>,
  clear: () => void,
): Promise<boolean> {
  if (s.nativeControlResponsePending.value) return false;
  s.nativeControlResponsePending.value = true;
  try {
    await s.respondControl(requestId, response);
    clear();
    return true;
  } catch (e) {
    pushRow(s, "assistant", `Control response failed: ${e}`);
    clear();
    return false;
  } finally {
    s.nativeControlResponsePending.value = false;
    s.sync();
  }
}

function clearSlot(s: InternalSession, request: Ref<CanUseToolReq | null>, markerId: Ref<number | null>, requestId: string) {
  if (request.value?.requestId !== requestId) return; // superseded by a newer request
  s.removeFeedMarker(markerId.value);
  markerId.value = null;
  request.value = null;
}

async function respond(s: InternalSession, answer: PendingAnswer): Promise<boolean> {
  const { chatId } = s;
  switch (answer.kind) {
    case "permission": {
      const cr = s.pendingPermission.value ?? s.pendingDiff.value;
      if (!cr) return false;
      const detail = (cr.input.command ?? cr.input.file_path ?? cr.input.path ?? cr.description ?? "") as string;
      const detailStr = detail ? ` — ${detail.length > 80 ? detail.slice(0, 80) + "…" : detail}` : "";
      const ok = await resolveNative(s, cr.requestId, answer.allow
        ? { behavior: "allow", updatedInput: answer.updatedInput ?? cr.input }
        : { behavior: "deny", message: answer.message || "User denied this action." }, () => {
        clearSlot(s, s.pendingPermission, s.pendingPermissionMsgId, cr.requestId);
        clearSlot(s, s.pendingDiff, s.pendingDiffMsgId, cr.requestId);
      });
      if (!ok) return false;
      if (answer.allow && answer.always) {
        const keys = ruleKeys(cr.toolName, cr.input);
        s.deps.host.addPermissionRule(keys[keys.length - 1]);
      }
      const label = answer.allow ? (answer.always ? "✓ Always allowed" : "✓ Allowed") : "✗ Denied";
      pushRow(s, "permission", `${label}: ${cr.toolName}${detailStr}`);
      return true;
    }
    case "question": {
      const cr = s.pendingQuestion.value;
      if (!cr) return false;
      // The tool reads input.answers keyed by question text; {} is the clean
      // "did not answer" dismiss (no error on the agent's side).
      return resolveNative(s, cr.requestId, { behavior: "allow", updatedInput: { ...cr.input, answers: answer.answers } },
        () => clearSlot(s, s.pendingQuestion, s.pendingQuestionMsgId, cr.requestId));
    }
    case "plan": {
      const cr = s.pendingPlan.value;
      if (!cr) return false;
      return resolveNative(s, cr.requestId, answer.approve
        ? { behavior: "allow", updatedInput: cr.input }
        : { behavior: "deny", message: answer.feedback?.trim() || "Keep planning — do not exit plan mode yet." },
        () => clearSlot(s, s.pendingPlan, s.pendingPlanMsgId, cr.requestId));
    }
    case "acpOption": {
      const r = s.acpPermReq.value;
      if (!r || s.permissionResponsePending.value) return false;
      s.permissionResponsePending.value = true;
      try {
        await s.deps.invoke("acp_respond_permission", { id: chatId, rpcId: r.rpcId, optionId: answer.optionId });
      } catch (e) {
        pushRow(s, "assistant", `Permission response failed: ${e}`);
        s.permissionResponsePending.value = false; // stays on screen, retryable
        return false;
      }
      pushRow(s, "permission", `${answer.reject ? "✗" : "✓"} ${answer.label}: ${r.title}`);
      // Codex acknowledges with serverRequest/resolved (onAcpData clears the
      // prompt then). A generic ACP adapter sends nothing of the sort — its
      // prompt used to sit on screen, and block eviction, forever.
      if (s.deps.host.chat(chatId)?.transport !== "codex-app-server" && s.acpPermReq.value === r) {
        s.removeFeedMarker(s.acpPermMsgId.value); s.acpPermMsgId.value = null;
        s.acpPermReq.value = null;
        s.acpPermRpcId.value = null;
        s.permissionResponsePending.value = false;
      }
      s.sync();
      return true;
    }
    case "codexInput": {
      const request = s.codexUserInput.value;
      if (!request || s.codexUserInputPending.value) return false;
      s.codexUserInputPending.value = true;
      try {
        await s.deps.invoke("acp_respond_user_input", { id: chatId, rpcId: request.rpcId, answers: answer.answers });
        s.codexUserInput.value = null;
        return true;
      } catch (e) {
        s.messages.value.push({ id: s.nextMsgId++, role: "assistant", text: `Unable to submit Codex input: ${e}` });
        s.codexUserInputPending.value = false;
        return false;
      } finally {
        s.sync();
      }
    }
  }
}
