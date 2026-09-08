import { defineStore } from "pinia";
import { reactive, ref } from "vue";
import { appTransport } from "@/lib/wailsCompat/core";
import type { Transport } from "@/runtime/transport";
import {
  clearRemoteCredentials,
  loadRemoteCredentials,
  pairDevice,
  saveRemoteCredentials,
  type RemoteCredentials,
} from "@/runtime/remoteEndpoint";
import type { TermStatus } from "@/lib/terminalStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import { smartTitle, isDefaultTitle } from "@/lib/chatTitle";

// The phone runs on the SAME transport, the same command table and the same
// event names as the desktop (src/runtime/transport.ts, src-wails/remoteapi.go).
// What used to live here — a second websocket client, a second reconnect loop,
// a second status derivation — is gone; api.ts with it.
//
// What is still mobile-specific and stays: the view stack, the WorkspaceGroup
// shape its views are written against, and the chat read model (chats arrive
// from remote_list_chats, and permission requests still come off the raw
// control channel, which is deliberately not part of the neutral event
// vocabulary — see CLAUDE.md).

export type TabStatus = TermStatus;

export interface WorkspaceGroup {
  id: number;
  name: string;
  path: string;
}

export interface RemoteMessage {
  id: number;
  role: "user" | "assistant" | "tool" | "thinking" | "permission" | "system-info" | "queued";
  text: string;
  partial?: boolean;
  toolInput?: Record<string, unknown>;
  toolOutput?: string;
  // Set from tool.started / tool.completed so a result can find its call —
  // the remote client renders the same tool cards the desktop does.
  toolUseId?: string;
  toolFailed?: boolean;
}

export interface PendingPermission {
  requestId?: string; // Claude control_request id
  rpcId?: number; // ACP JSON-RPC id
  toolName: string;
  detail: string;
  // ACP/Codex option ids the response must pick from — Codex's raw JSON-RPC
  // carries no options array, so those are fabricated (mirrors
  // src/lib/acpParser.ts's parseAcpPermRequest); generic ACP's are read
  // verbatim from params.options since they're provider-defined.
  options: { optionId: string; kind: string }[];
  // The tool's original arguments (Claude only) — must be echoed back verbatim
  // on allow, since the protocol executes the tool with whatever
  // `updatedInput` the response carries (see respondChatPermission).
  input?: Record<string, unknown>;
}

export interface RemoteChat {
  id: number;
  workspaceId: number;
  title: string;
  busy: boolean;
  status?: TabStatus | null;
  agentKind?: string | null;
  transport: "claude-cli" | "codex-app-server" | "acp";
  claudeSessionId: string;
  // The model picked when this chat was created (remoteChatShape's "model"
  // column) — echoed back on every refresh, unlike effort/permissionMode
  // which aren't persisted per-chat anywhere.
  model?: string;
  workspaceName?: string;
  workspacePath?: string;
  messages: RemoteMessage[];
  // Set when a turn finished while this chat was not the open one — cleared
  // by markChatSeen(). Mirrors desktop's "review" persisting until the tab
  // is seen (Terminal.vue's settleDone()).
  unseen?: "review" | "error";
  // Set when the agent is blocked on an allow/deny decision — mirrors
  // desktop's "permission" status. Cleared by respondChatPermission().
  pendingPermission?: PendingPermission | null;
}

export type View = "connect" | "chats" | "chat" | "welcome";

// Mirrors desktop's settled/live split (src/stores/claudeChats.ts
// AUTO_SETTLE_AFTER_DAYS/settledFor) so the mobile chat list groups the same
// way — but as a mobile-side heuristic, not synced data: lastActivityAt and
// the manual settledOverride both live only in this device's localStorage,
// same reasoning as burrow.seenAt.mobile above.
const AUTO_SETTLE_AFTER_DAYS = 3;
const DAY_MS = 24 * 60 * 60 * 1000;
const LAST_ACTIVITY_KEY = "burrow.chatActivity.mobile";
const SETTLED_OVERRIDE_KEY = "burrow.chatSettledOverride.mobile";

function loadJsonRecord<T>(key: string): Record<string, T> {
  try {
    return JSON.parse(localStorage.getItem(key) ?? "{}") as Record<string, T>;
  } catch {
    return {};
  }
}

export const useRemoteStore = defineStore("remote", () => {
  const credentials = ref<RemoteCredentials | null>(loadRemoteCredentials());
  const baseUrl = ref(credentials.value?.baseUrl ?? "");
  const connected = ref(false);
  const connecting = ref(false);
  const connectError = ref("");
  const reconnecting = ref(false);

  const view = ref<View>(credentials.value ? "chats" : "connect");
  const workspaces = ref<WorkspaceGroup[]>([]);
  const loading = ref(false);
  const listError = ref("");
  const chats = ref<RemoteChat[]>([]);
  const activeChat = ref<RemoteChat | null>(null);
  const chatActivity = reactive<Record<string, number>>(loadJsonRecord(LAST_ACTIVITY_KEY));
  const chatSettledOverride = reactive<Record<string, "settled" | "active">>(loadJsonRecord(SETTLED_OVERRIDE_KEY));

  let transport: Transport | null = null;
  // Every listen() this store installed, so a disconnect() really stops
  // listening rather than leaving handlers to fire against torn-down state.
  const unlisteners: Array<() => void> = [];
  const watchedChats = new Set<number>();

  function track(off: () => void) {
    unlisteners.push(off);
  }

  // Debounces the workspaces-changed → refresh() below. A single repo action
  // can fire several emits back to back — deleting a repo with N worktrees
  // is N+1 workspaces-changed events, one per child removal plus the parent
  // (src/stores/workspace.ts's remove() deletes them one at a time) — and
  // each refresh() costs two full RPCs, one of them a shell_snapshot
  // carrying every workspace's icon as a base64 data URL. noteSeq is
  // monotonic (src/runtime/transport.ts), so coalescing these into one
  // trailing refresh cannot rewind the client's position; the only exposure
  // a debounce adds is an out-of-order apply, which the very next event
  // already corrects.
  const WORKSPACES_CHANGED_DEBOUNCE_MS = 300;
  let workspacesChangedTimer: ReturnType<typeof setTimeout> | null = null;
  function scheduleWorkspacesRefresh() {
    if (workspacesChangedTimer !== null) clearTimeout(workspacesChangedTimer);
    workspacesChangedTimer = setTimeout(() => {
      workspacesChangedTimer = null;
      void refresh();
    }, WORKSPACES_CHANGED_DEBOUNCE_MS);
  }

  function chatStatus(chat: RemoteChat): TabStatus {
    if (chat.pendingPermission) return "permission";
    if (chat.busy) return "running";
    if (chat.unseen) return chat.unseen;
    return "idle";
  }

  function touchChatActivity(chatId: number) {
    chatActivity[String(chatId)] = Date.now();
    try {
      localStorage.setItem(LAST_ACTIVITY_KEY, JSON.stringify(chatActivity));
    } catch { /* private mode / quota — settling degrades to "always fresh", not fatal */ }
  }

  /** Ported from desktop's settledFor(): pending work always wins (not
   *  settled), then a manual pin, then — only past AUTO_SETTLE_AFTER_DAYS of
   *  inactivity — auto-settled. A chat never locally observed yet (no
   *  chatActivity entry) is treated as fresh, same as one that just finished:
   *  it ages into the shelf rather than snapping there on first load. */
  function chatSettled(chat: RemoteChat): boolean {
    const status = chatStatus(chat);
    if (chat.busy || status === "running" || status === "waiting" || status === "permission") return false;
    const override = chatSettledOverride[String(chat.id)];
    if (override === "settled") return true;
    if (override === "active") return false;
    const last = chatActivity[String(chat.id)];
    if (last === undefined) return false;
    return Date.now() - last >= AUTO_SETTLE_AFTER_DAYS * DAY_MS;
  }

  function setChatSettledOverride(chatId: number, value: "settled" | "active" | null) {
    if (value === null) delete chatSettledOverride[String(chatId)];
    else chatSettledOverride[String(chatId)] = value;
    try {
      localStorage.setItem(SETTLED_OVERRIDE_KEY, JSON.stringify(chatSettledOverride));
    } catch { /* private mode / quota */ }
  }

  // ── connection ────────────────────────────────────────────────────────────

  /** Pair with a six-digit code, then connect with the token it returns. */
  async function pair(url: string, code: string): Promise<void> {
    connecting.value = true;
    connectError.value = "";
    try {
      const creds = await pairDevice(url, code, deviceName(), deviceKind());
      credentials.value = creds;
      baseUrl.value = creds.baseUrl;
      await connect();
    } catch (e: any) {
      connectError.value = e?.message ?? "Pairing failed";
      throw e;
    } finally {
      connecting.value = false;
    }
  }

  function deviceName(): string {
    const ua = typeof navigator === "undefined" ? "" : navigator.userAgent;
    // Enough to tell the rows apart in desktop Settings, which is the whole
    // job here — a list of "Paired device" entries is not actionable.
    if (/iPhone/i.test(ua)) return "iPhone";
    if (/iPad/i.test(ua)) return "iPad";
    if (/Android/i.test(ua)) return "Android phone";
    return "Browser";
  }

  function deviceKind(): string {
    const ua = typeof navigator === "undefined" ? "" : navigator.userAgent;
    if (/iPad|Tablet/i.test(ua)) return "tablet";
    if (/iPhone|Android/i.test(ua)) return "phone";
    return "browser";
  }

  /**
   * Attach to the shared transport and take a first snapshot.
   *
   * There is no reconnect loop here any more: the transport owns backoff,
   * ticket renewal and the queue, and `onState` is how this store learns what
   * happened. A resync means the gap outran the server's replay ring, so the
   * only honest recovery is a fresh snapshot.
   */
  async function connect(): Promise<void> {
    if (!credentials.value) {
      credentials.value = loadRemoteCredentials();
      if (!credentials.value) throw new Error("this device is not paired");
    }
    if (transport) {
      await refresh();
      return;
    }
    connecting.value = true;
    connectError.value = "";
    try {
      transport = appTransport();
      track(
        transport.onState((up) => {
          connected.value = up;
          reconnecting.value = !up;
          if (up) {
            // Every reconnect refreshes: resume replays the numbered events,
            // but a phone that was asleep for an hour is past the ring more
            // often than not, and a refresh is cheap next to being wrong.
            void refresh();
          }
        }),
      );
      track(transport.onResync(() => void refresh()));
      // A chat created on the desktop (or on another device) now reaches this
      // client without a reload. The mobile store used to subscribe to
      // `remote-chats` for this, a name NOTHING in the tree ever emitted — so
      // cross-client chat creation has never worked in either direction until
      // the list moved into SQLite and got a real event.
      track(transport.listen("chats-changed", () => void loadChats()));
      // A workspace created/renamed/deleted/reordered on the desktop (or by
      // another device) now reaches this client without a reload. Nothing
      // ever emitted this event before — shell_snapshot only ran on connect,
      // so an already-connected client had no way to learn the list changed.
      // refresh() takes a whole new snapshot, so there is no echo to filter:
      // this client only reads workspaces, it never mutates them.
      track(transport.listen("workspaces-changed", scheduleWorkspacesRefresh));
      if (view.value === "connect") view.value = "chats";
      await refresh();
    } catch (e: any) {
      connectError.value = e?.message ?? "Connection failed";
      throw e;
    } finally {
      connecting.value = false;
    }
  }

  function disconnect() {
    for (const off of unlisteners.splice(0)) off();
    if (workspacesChangedTimer !== null) {
      clearTimeout(workspacesChangedTimer);
      workspacesChangedTimer = null;
    }
    watchedChats.clear();
    transport?.close();
    transport = null;
    clearRemoteCredentials();
    credentials.value = null;
    connected.value = false;
    reconnecting.value = false;
    workspaces.value = [];
    chats.value = [];
    activeChat.value = null;
    view.value = "connect";
  }

  // ── first paint ───────────────────────────────────────────────────────────

  /**
   * One `shell_snapshot` for the whole shell: workspaces and chats, plus the
   * `seq` that state is current as of, which is what a later resume counts
   * from. Mobile only shows chats now (no terminal-tab or phase UI left), so
   * this reads just the workspace list (for the chat-creation picker) and the
   * chat list — not snap.tabs/snap.phases/list_pty_sessions, which existed
   * only to feed the terminal views that were removed.
   */
  async function refresh(): Promise<void> {
    if (!transport) return;
    loading.value = true;
    listError.value = "";
    try {
      const snap = await transport.invoke<ShellSnapshotData>("shell_snapshot");
      // Before applying, so an event landing during the apply is not counted
      // as already seen (the server takes seq before its own data read for
      // the same reason).
      transport.noteSeq(snap.seq ?? 0);

      workspaces.value = (snap.workspaces ?? []).map((ws) => ({ id: ws.id, name: ws.name, path: ws.path }));
      applyChats((snap.chats ?? []) as unknown as RemoteChat[]);
    } catch (e: any) {
      listError.value = e?.message ?? "Failed to load";
    } finally {
      loading.value = false;
    }
  }

  /** Kept as the name views call to force a reload. */
  const loadSessions = refresh;

  // ── chats ─────────────────────────────────────────────────────────────────

  function chatFor(id: number) {
    return chats.value.find((chat) => chat.id === id);
  }

  function applyChats(incoming: RemoteChat[]) {
    const next = (incoming ?? []).map((chat) => {
      const existing = chatFor(chat.id);
      // Preserve the live transcript and turn state across a refresh: the
      // snapshot's chat records carry metadata, not messages, so taking them
      // verbatim would wipe a streaming turn on every reconnect.
      return existing
        ? Object.assign(existing, { ...chat, messages: existing.messages })
        : { ...chat, messages: Array.isArray(chat.messages) ? chat.messages : [] };
    });
    chats.value = next;
    for (const chat of next) {
      watchChat(chat);
      // First observation of this chat on this device: seed a baseline so it
      // doesn't snap straight to "settled" for having no recorded activity.
      if (chatActivity[String(chat.id)] === undefined) touchChatActivity(chat.id);
    }
  }

  function safeJson(raw: string): unknown {
    try {
      return JSON.parse(raw);
    } catch {
      return null;
    }
  }

  function appendRemoteText(chat: RemoteChat, role: "assistant" | "thinking", text: string, partial = true) {
    if (!text) return;
    const last = chat.messages[chat.messages.length - 1];
    if (last?.role === role && last.partial) last.text += text;
    else chat.messages.push({ id: Date.now() + chat.messages.length, role, text, partial });
  }

  // One applier for both runtimes. The wire formats are read on the Go side
  // (src-wails/providerruntime.go) and arrive as provider-neutral events, so a
  // remote client no longer re-implements stream-json and ACP to its own,
  // shallower depth than the desktop.
  /**
   * Prompts this client sent and has not yet seen echoed back.
   *
   * Go publishes the human's prompt through emitChatLine now, so it reaches
   * every client — including the one that typed it, which already drew the
   * bubble locally (and locally is the only place the attached images exist,
   * since the stream records the text). Matching the echo against this set is
   * what keeps the sender from drawing it twice.
   *
   * ponytail: matched on text, not on an id, because the id is the stream ord
   * and the sender does not learn it — ClaudeSend returns no ord today.
   * Ceiling: sending the identical text twice inside one round trip collapses
   * to one bubble until the next reload. Thread the ord back through
   * claude_send/acp_send if that ever matters.
   */
  const pendingSends = new Set<string>();

  function applyEvent(chat: RemoteChat, event: Record<string, any>) {
    touchChatActivity(chat.id);
    // Only these events happen strictly during an active turn — a chat
    // driven from the desktop (or another remote client) never runs sendChat
    // on this client, so chat.busy would otherwise stay false the whole time
    // and the chat would look idle (and get hidden by the settled-chats
    // declutter) while it is actually streaming. tool.completed is excluded:
    // it can arrive after the fact and says nothing about whether a turn is
    // currently running.
    if (["text.delta", "thinking.delta", "tool.started"].includes(event.type) && !chat.busy) chat.busy = true;
    switch (event.type) {
      case "text.delta":
        appendRemoteText(chat, "assistant", event.text ?? "");
        return;
      case "thinking.delta":
        appendRemoteText(chat, "thinking", event.text ?? "");
        return;
      case "user.delta": {
        // A prompt from ANOTHER client (or a replay of one) — this is what
        // makes a message typed on the desktop show up here at all.
        const text = event.text ?? "";
        if (pendingSends.delete(text)) return; // our own echo; already on screen
        chat.messages.push({ id: Date.now() + chat.messages.length, role: "user", text });
        return;
      }
      case "tool.started":
        chat.messages.push({
          id: Date.now() + chat.messages.length,
          role: "tool",
          text: event.name ?? "Tool",
          toolInput: event.input ?? {},
          toolUseId: event.toolCallId,
        });
        return;
      case "tool.completed": {
        const tool = [...chat.messages].reverse().find((m) => m.toolUseId === event.toolCallId);
        if (tool) {
          tool.toolOutput = event.output ?? "";
          tool.toolFailed = event.failed === true;
        }
        return;
      }
      case "turn.completed":
      case "turn.failed": {
        // A turn ending is the one event guaranteed to mean "whatever was
        // pending is no longer pending" — regardless of whether mobile,
        // desktop, or nobody resolved it. Without this, a permission the
        // desktop answered (or one bypassed by the turn otherwise moving on)
        // would stay stuck forever, pinning this chat to the top of the list
        // and permanently disabling its composer.
        chat.pendingPermission = null;
        chat.busy = false;
        chat.messages.forEach((message) => {
          message.partial = false;
        });
        const watching = view.value === "chat" && activeChat.value?.id === chat.id;
        if (!watching) chat.unseen = event.type === "turn.failed" ? "error" : "review";
        return;
      }
      case "session.id":
        if (typeof event.sessionId === "string") chat.claudeSessionId = event.sessionId;
        return;
    }
  }

  // Codex's raw JSON-RPC approval requests carry no options array — the
  // desktop's src/lib/acpParser.ts (parseAcpPermRequest) fabricates this
  // fixed 3-item set and Go's AcpRespondPermission (control.go) only
  // recognizes these exact optionId strings for a Codex session. Mirrored
  // here rather than reinvented so mobile answers the same way desktop does.
  const CODEX_APPROVAL_METHODS = [
    "item/commandExecution/requestApproval",
    "item/fileChange/requestApproval",
    "item/permissions/requestApproval",
  ];
  const CODEX_APPROVAL_OPTIONS = [
    { optionId: "codex:accept", kind: "allow_once" },
    { optionId: "codex:acceptForSession", kind: "allow_always" },
    { optionId: "codex:decline", kind: "reject_once" },
  ];

  /**
   * Subscribe a chat to its event batch and its permission channel.
   * Idempotent, because every refresh walks the whole chat list.
   *
   * The permission channel stays RAW (claude-data-* / acp-req-*) on purpose:
   * the control/permission protocol is deliberately not part of the neutral
   * ProviderRuntimeEvent vocabulary — it is a UI decision, not transcript.
   */
  function watchChat(chat: RemoteChat) {
    const id = chat.id;
    if (watchedChats.has(id)) return;
    watchedChats.add(id);

    track(
      transport!.listen<{ events?: Array<Record<string, any>> }>(`chat-event-${id}`, (payload) => {
        const batch = (typeof payload === "string" ? safeJson(payload) : payload) as
          | { events?: Array<Record<string, any>> }
          | null;
        // Mutate the array's reactive element, not the plain object this
        // closure captured at subscribe time — the latter never goes through
        // Pinia's proxy, so busy/messages/unseen do change but Vue's render
        // effect never reruns (the DOM silently stops matching the data).
        const live = chatFor(id);
        if (!live) return;
        for (const event of batch?.events ?? []) applyEvent(live, event);
      }),
    );

    const transportKind = chat.transport;
    const rawEvent = transportKind === "claude-cli" ? `claude-data-${id}` : `acp-req-${id}`;
    track(
      transport!.listen(rawEvent, (payload) => {
        // The payload is the ChatStreamLine envelope Go's emitChatLine emits
        // ({ord, kind, line}) — `line` is a STRING holding the raw protocol
        // JSON, not the message itself. Unwrap it before reading
        // msg.type/.request/.id/.method, or every field reads undefined and
        // no permission is ever detected.
        const envelope = (typeof payload === "string" ? safeJson(payload) : payload) as { line?: string } | null;
        const parsed = typeof envelope?.line === "string" ? safeJson(envelope.line) : null;
        if (!parsed || typeof parsed !== "object") return;
        const msg = parsed as Record<string, any>;
        const live = chatFor(id);
        if (!live) return;

        if (transportKind === "claude-cli") {
          if (msg.type !== "control_request" || msg.request?.subtype !== "can_use_tool") return;
          const input = msg.request.input ?? {};
          const detail = input.command ?? input.file_path ?? input.path ?? "";
          live.pendingPermission = {
            requestId: msg.request_id,
            toolName: msg.request.tool_name ?? "Tool",
            detail: String(detail),
            options: [],
            input,
          };
          return;
        }

        // ACP: a server->client REQUEST has both method and id.
        if (typeof msg.id !== "number" || !msg.method) return;

        if (CODEX_APPROVAL_METHODS.includes(msg.method)) {
          const p = msg.params ?? {};
          const command = typeof p.command === "string" ? p.command : "";
          const toolName = msg.method.includes("commandExecution")
            ? "Run command"
            : msg.method.includes("fileChange")
              ? "Apply file changes"
              : "Grant additional permission";
          live.pendingPermission = { rpcId: msg.id, toolName, detail: command, options: CODEX_APPROVAL_OPTIONS };
          return;
        }

        if (msg.method !== "session/request_permission") return;
        const options = (msg.params?.options ?? []).map((o: any) => ({ optionId: o.optionId, kind: o.kind }));
        live.pendingPermission = {
          rpcId: msg.id,
          toolName: msg.params?.toolCall?.title ?? msg.params?.title ?? "Tool",
          detail: "",
          options,
        };
      }),
    );
  }

  async function loadChats() {
    if (!transport) return;
    try {
      applyChats(await transport.invoke<RemoteChat[]>("remote_list_chats"));
    } catch (e: any) {
      listError.value = e?.message ?? "Failed to load chats";
    }
  }

  function openChat(chat: RemoteChat) {
    activeChat.value = chat;
    view.value = "chat";
    markChatSeen(chat.id);
  }

  function markChatSeen(chatId: number) {
    const chat = chatFor(chatId);
    if (chat) chat.unseen = undefined;
  }

  function closeChat() {
    activeChat.value = null;
    view.value = "chats";
  }

  // Mirrors AgentChat.vue's smartTitle→refineTitle: a cheap local heuristic
  // the instant the first prompt goes out, then (fire-and-forget) a
  // model-written upgrade from generate_chat_title. Only ever touches a
  // still-default "Chat N"/"Chat N (phone)" title — a chat someone already
  // named (by hand, once renaming exists, or an earlier refine) is left
  // alone. remote_set_chat_title's expectTitle is a compare-and-swap: it
  // only replaces the title this call actually saw, so a second send fired
  // before the first refine lands can't stomp a newer title with a stale one.
  async function autoTitleChat(chat: RemoteChat, prompt: string) {
    if (!transport || !isDefaultTitle(chat.title)) return;
    const heuristic = smartTitle(prompt);
    const priorTitle = chat.title;
    try {
      await transport.invoke("remote_set_chat_title", { id: chat.id, title: heuristic, expectTitle: priorTitle });
    } catch {
      return;
    }
    chat.title = heuristic;

    const cwd = workspaces.value.find((w) => w.id === chat.workspaceId)?.path ?? "";
    let refined = "";
    try {
      refined = await transport.invoke<string>("generate_chat_title", { cwd, model: "", policy: "", text: prompt });
    } catch {
      return;
    }
    if (!refined || refined === heuristic) return;
    try {
      await transport.invoke("remote_set_chat_title", { id: chat.id, title: refined, expectTitle: heuristic });
    } catch {
      return;
    }
    if (chat.title === heuristic) chat.title = refined;
  }

  async function sendChat(text: string) {
    const chat = activeChat.value;
    if (!transport || !chat || !text.trim() || chat.busy) return;
    const prompt = text.trim();
    touchChatActivity(chat.id);
    chat.messages.push({ id: Date.now(), role: "user", text: prompt });
    void autoTitleChat(chat, prompt);
    // Claim the echo before the call goes out, or a fast round trip lands
    // user.delta while we are still awaiting and draws a second bubble.
    pendingSends.add(prompt);
    chat.busy = true;
    try {
      if (chat.transport === "claude-cli") {
        // AgentChat.vue's onMounted does this before ever sending; the phone
        // never mounts that component, so an older chat whose Go-side process
        // already exited (ReapIdle, or simply never spawned by this backend)
        // sent straight into agentproc.Manager.Send with nothing registered —
        // "unknown agent session" while the prompt still broadcast fine,
        // because ClaudeSend publishes it before writing to stdin. ClaudeStart
        // is a no-op if the session is already alive, so this is safe on
        // every send, not just the first. It does NOT apply the model/
        // effort/permissionMode picked in WelcomeView's composer — those
        // only take effect once, inside RemoteCreateChat's own ClaudeStart
        // call (remote.go), which is what actually spawns a brand-new
        // chat's CLI. This later call is purely a resume path: chat.model
        // is the one piece of that original config the server persists and
        // echoes back (remoteChatShape's "model" key), so it's the
        // best-effort value to resume with if this backend never started
        // the process at all.
        // ponytail: permissionMode/effort/appendSystemPrompt/configDir/
        // profileCommand/profileArgs aren't threaded on resume — mobile has
        // no per-chat persistence for them (only `model` is a real column).
        // Fine for a resume of an already-alive session (no-op regardless);
        // would under-configure a resume of a session this backend never
        // started. Thread real chat config through if that matters.
        await transport.invoke("claude_start", {
          id: String(chat.id),
          cwd: workspaces.value.find((w) => w.id === chat.workspaceId)?.path ?? "",
          resumeSessionId: chat.claudeSessionId || "",
          permissionMode: "",
          appendSystemPrompt: "",
          model: chat.model ?? "",
          effort: "",
          configDir: "",
          profileCommand: "",
          profileArgs: "",
        });
        await transport.invoke("claude_send", {
          id: String(chat.id),
          text: text.trim(),
          sessionId: chat.claudeSessionId || null,
        });
      } else {
        // Same class of bug as claude-cli above, reproduced against a live
        // backend: an old codex-app-server chat with no process registered
        // failed acp_send with "acp adapter not running". CodexStart resolves
        // its own binary (resolveAgentBin("codex", cwd)) and is a no-op if
        // already alive, so — unlike generic "acp" below — it needs nothing
        // mobile doesn't already have.
        // ponytail: plain "acp" transport (custom adapters) is NOT covered —
        // AcpStart needs the adapter's command/args/kind, which only lives in
        // the desktop's per-project agent config. Thread that through if a
        // custom-adapter chat needs mobile resume too.
        if (chat.transport === "codex-app-server") {
          await transport.invoke("codex_start", {
            id: String(chat.id),
            cwd: workspaces.value.find((w) => w.id === chat.workspaceId)?.path ?? "",
            env: {},
            resumeSessionId: chat.claudeSessionId || "",
          });
        }
        await transport.invoke("acp_send", { id: String(chat.id), text: text.trim() });
      }
    } catch (e: any) {
      chat.busy = false;
      // Release the claim: the send failed, so no echo is coming. Leaving it
      // set would swallow the NEXT identical prompt's echo — including one
      // typed on the desktop.
      pendingSends.delete(prompt);
      chat.messages.push({ id: Date.now() + 1, role: "assistant", text: `Chyba odeslání: ${e?.message ?? e}` });
    }
  }

  // agentKind is "claude" only — RemoteCreateChat (remote.go) rejects
  // anything else today (an ACP/Codex session needs command/args/configDir
  // that only exist in the desktop's per-project agent config). WelcomeView's
  // model picker only offers Claude models for the same reason: a Codex
  // choice would reach this call and fail every time.
  //
  // model/effort/permissionMode are WelcomeView's composer choices, passed
  // straight through to remote_create_chat so they reach the SAME
  // ClaudeStart call that spawns the CLI (see remote.go) — the only place
  // they can take effect, since every later claude_start (sendChat, below)
  // is a no-op once the session is alive.
  async function createChat(
    workspaceId: number,
    agentKind: "claude",
    model = "",
    effort = "",
    permissionMode = "",
  ): Promise<RemoteChat> {
    if (!transport) throw new Error("not connected");
    const chat = await transport.invoke<RemoteChat>("remote_create_chat", {
      workspaceId,
      agentKind,
      model,
      effort,
      permissionMode,
    });
    chats.value.push({ ...chat, messages: [] });
    watchChat(chat);
    const stored = chatFor(chat.id)!;
    openChat(stored);
    // Returned so a caller (WelcomeView) can immediately sendChat() the
    // prompt into the SAME reactive object this store now holds.
    return stored;
  }

  async function respondChatPermission(chatId: number, allow: boolean) {
    const chat = chatFor(chatId);
    const pending = chat?.pendingPermission;
    if (!transport || !chat || !pending) return;
    chat.pendingPermission = null;
    try {
      if (pending.requestId) {
        await transport.invoke("claude_respond_control", {
          id: String(chat.id),
          requestId: pending.requestId,
          response: allow
            // Echo the tool's original arguments — the protocol executes the
            // tool with whatever updatedInput is sent, so sending {} would
            // silently strip the command/file_path/etc the user was shown
            // and approved (mirrors AgentChat.vue's opts?.updatedInput ?? cr.input).
            ? { behavior: "allow", updatedInput: pending.input ?? {} }
            : { behavior: "deny", message: "User denied this action." },
        });
      } else if (pending.rpcId !== undefined) {
        // optionIds are agent-defined — pick the matching one by kind from
        // the request's own options (NOT a hardcoded string), same as
        // AgentChat.vue's respondPermission. Only fall back to a bare
        // "allow_once"/"reject_once" string if options came up empty.
        const pick = (...kinds: string[]) => {
          for (const k of kinds) {
            const o = pending.options.find((x) => x.kind === k);
            if (o) return o.optionId;
          }
          return pending.options[0]?.optionId ?? (allow ? "allow_once" : "reject_once");
        };
        const optionId = allow ? pick("allow_once", "allow_always") : pick("reject_once", "reject_always");
        await transport.invoke("acp_respond_permission", { id: String(chat.id), rpcId: pending.rpcId, optionId });
      }
    } catch (e: any) {
      chat.messages.push({ id: Date.now(), role: "assistant", text: `Odpověď na povolení selhala: ${e?.message ?? e}` });
    }
  }

  // ── views ─────────────────────────────────────────────────────────────────

  function showChats() {
    view.value = "chats";
  }
  function showWelcome() {
    view.value = "welcome";
  }

  return {
    baseUrl, credentials, connected, connecting, connectError, reconnecting,
    view, workspaces, loading, listError,
    chats, activeChat, chatActivity,
    pair, connect, disconnect, loadSessions, refresh, loadChats,
    showChats, showWelcome,
    openChat, closeChat, sendChat, createChat, chatSettled, setChatSettledOverride,
    chatStatus,
    markChatSeen, respondChatPermission,
    saveRemoteCredentials,
  };
});
