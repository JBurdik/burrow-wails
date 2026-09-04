import { defineStore } from "pinia";
import { reactive, ref } from "vue";
import { BurrowWsClient } from "./api";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import type { TermStatus } from "@/lib/terminalStatus";

const URL_LEGACY_KEY = "burrow-mobile-url";
const TOKEN_LEGACY_KEY = "burrow-mobile-token";
const URL_CONFIG_KEY = "mobileBaseUrl";
const TOKEN_CONFIG_KEY = "mobileToken";

export type TabStatus = TermStatus;

export interface Tab {
  ptyId: number;
  title: string;
  cwd: string;
  workspaceId: number;
  workspaceName: string;
}

// Synthetic group for live PTYs the workspace tables do not know about.
export const LIVE_GROUP_ID = -1;

export interface WorkspaceGroup {
  id: number;
  name: string;
  path: string;
  tabs: Tab[];
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
  rpcId?: number;      // ACP JSON-RPC id
  toolName: string;
  detail: string;
  // ACP/Codex option ids the response must pick from — Codex's raw JSON-RPC
  // carries no options array, so those are fabricated (mirrors
  // src/lib/acpParser.ts's parseAcpPermRequest); generic ACP's are read
  // verbatim from params.options since they're provider-defined.
  options: { optionId: string; kind: string }[];
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

export type View = "connect" | "dashboard" | "chats" | "chat" | "sessions" | "terminal";

export const useRemoteStore = defineStore("remote", () => {
  const baseUrl = ref("");
  const token = ref("");
  configReady.then(() => {
    migrateFromLocalStorage(URL_LEGACY_KEY, URL_CONFIG_KEY);
    migrateFromLocalStorage(TOKEN_LEGACY_KEY, TOKEN_CONFIG_KEY);
    baseUrl.value = getConfig<string>(URL_CONFIG_KEY, "");
    token.value = getConfig<string>(TOKEN_CONFIG_KEY, "");
  });
  const connected = ref(false);
  const connecting = ref(false);
  const connectError = ref("");

  const view = ref<View>("connect");
  const workspaces = ref<WorkspaceGroup[]>([]);
  const statuses = reactive(new Map<number, TabStatus>());
  const loading = ref(false);
  const listError = ref("");
  const activeTab = ref<Tab | null>(null);
  const chats = ref<RemoteChat[]>([]);
  const activeChat = ref<RemoteChat | null>(null);

  const reconnecting = ref(false);
  let reconnectAttempt = 0;
  let reconnectTimer: number | undefined;
  // Bumped on every connect() call and on disconnect(), so an in-flight
  // connect() (its healthCheck/WS handshake can each take seconds) can tell,
  // right before it commits success, whether a newer connect() or an
  // explicit disconnect() superseded it in the meantime.
  let connectGeneration = 0;

  let client: BurrowWsClient | null = null;
  const doneTimers = new Map<number, number>();

  function statusFor(ptyId: number): TabStatus {
    return statuses.get(ptyId) ?? "idle";
  }

  function chatStatus(chat: RemoteChat): TabStatus {
    if (chat.pendingPermission) return "permission";
    if (chat.busy) return "running";
    if (chat.unseen) return chat.unseen;
    return "idle";
  }

  function watchTabStatus(ptyId: number) {
    client?.subscribe(`pty-hook-${ptyId}`, (payload) => {
      // Broadcast branch only ever sends the bare state string (see api.ts note).
      const state = typeof payload === "string" ? payload : payload?.state;
      if (state === "running" || state === "waiting" || state === "permission") {
        const t = doneTimers.get(ptyId);
        if (t !== undefined) { window.clearTimeout(t); doneTimers.delete(ptyId); }
        statuses.set(ptyId, state);
      } else if (state === "error") {
        const t = doneTimers.get(ptyId);
        if (t !== undefined) { window.clearTimeout(t); doneTimers.delete(ptyId); }
        statuses.set(ptyId, "error"); // persists until markTabSeen, like desktop
      } else if (state === "done") {
        const watching = view.value === "terminal" && activeTab.value?.ptyId === ptyId;
        if (watching) {
          statuses.set(ptyId, "done");
          const t = window.setTimeout(() => statuses.set(ptyId, "idle"), 4000);
          doneTimers.set(ptyId, t);
        } else {
          statuses.set(ptyId, "review");
        }
      }
    });
  }

  // Pair with a six-digit code, then connect with the token it returns.
  // connect() persists that token, so this only happens on the first visit.
  async function pair(url: string, code: string): Promise<void> {
    const normalized = url.replace(/\/$/, "");
    const tok = await BurrowWsClient.pair(normalized, code);
    await connect(normalized, tok);
  }

  async function connect(url: string, tok: string): Promise<void> {
    const myGeneration = ++connectGeneration;
    connecting.value = true;
    connectError.value = "";
    const normalized = url.replace(/\/$/, "");
    try {
      const ok = await BurrowWsClient.healthCheck(normalized);
      if (!ok) throw new Error("Server reachable but /healthz did not return 200");

      const c = new BurrowWsClient();
      await c.connect(normalized, tok);
      c.onClose = () => {
        connected.value = false;
        if (view.value === "terminal") view.value = "dashboard";
        scheduleReconnect();
      };
      // A newer connect() call or an explicit disconnect() ran while the
      // above awaits were in flight — don't resurrect state disconnect()
      // just tore down, and don't leak the socket we just opened.
      if (myGeneration !== connectGeneration) { c.close(); return; }
      client = c;
      connected.value = true;
      baseUrl.value = normalized;
      token.value = tok;
      setConfig(URL_CONFIG_KEY, normalized);
      setConfig(TOKEN_CONFIG_KEY, tok);
      view.value = "dashboard";
      await Promise.all([loadSessions(), loadChats()]);
    } catch (e: any) {
      // Only clobber shared state if this is still the current attempt — a
      // stale call failing after a newer one already succeeded must not
      // flip a live connection back to disconnected/error.
      if (myGeneration === connectGeneration) {
        connectError.value = e?.message ?? "Connection failed";
        connected.value = false;
      }
      throw e;
    } finally {
      // Same guard: a superseded call's finally must not clear the
      // "connecting" indicator while a newer call is still in flight.
      if (myGeneration === connectGeneration) connecting.value = false;
    }
  }

  function scheduleReconnect() {
    if (reconnectTimer !== undefined || view.value === "connect") return;
    reconnecting.value = true;
    const delay = Math.min(1000 * 2 ** reconnectAttempt, 30000);
    reconnectTimer = window.setTimeout(async () => {
      reconnectTimer = undefined;
      if (!baseUrl.value || !token.value) { reconnecting.value = false; return; }
      try {
        await connect(baseUrl.value, token.value);
        reconnectAttempt = 0;
        reconnecting.value = false;
      } catch {
        reconnectAttempt++;
        scheduleReconnect();
      }
    }, delay);
  }

  function disconnect() {
    connectGeneration++;
    if (reconnectTimer !== undefined) { window.clearTimeout(reconnectTimer); reconnectTimer = undefined; }
    reconnectAttempt = 0;
    reconnecting.value = false;
    client?.close();
    client = null;
    connected.value = false;
    workspaces.value = [];
    statuses.clear();
    chats.value = [];
    activeChat.value = null;
    view.value = "connect";
  }

  async function loadSessions() {
    if (!client) return;
    loading.value = true;
    listError.value = "";
    try {
      const wss: { id: number; name: string; path: string }[] = await client.call("list_workspaces");
      const groups: WorkspaceGroup[] = [];
      for (const ws of wss) {
        const tabs: any[] = (await client.call("list_terminal_tabs", { workspaceId: ws.id })) ?? [];
        const liveTabs: Tab[] = tabs
          .filter((t) => typeof t.pty_id === "number")
          .map((t) => ({
            ptyId: t.pty_id,
            title: t.title || t.default_title || `PTY ${t.pty_id}`,
            cwd: t.cwd ?? ws.path,
            workspaceId: ws.id,
            workspaceName: ws.name,
          }));
        groups.push({ id: ws.id, name: ws.name, path: ws.path, tabs: liveTabs });
        for (const t of liveTabs) {
          if (!statuses.has(t.ptyId)) statuses.set(t.ptyId, "idle");
          watchTabStatus(t.ptyId);
        }
      }
      // A tab only reaches SQLite when the desktop saves the workspace, so a
      // freshly spawned PTY can be live while absent from every group. Ask the
      // daemon what it is actually holding and surface the leftovers, rather
      // than showing an empty list next to a running agent.
      const known = new Set(groups.flatMap((g) => g.tabs.map((t) => t.ptyId)));
      const live: string[] = (await client.call("list_pty_sessions").catch(() => [])) ?? [];
      const orphans: Tab[] = live
        .map((id) => Number(id))
        .filter((id) => Number.isFinite(id) && !known.has(id))
        .map((id) => ({
          ptyId: id,
          title: `PTY ${id}`,
          cwd: "",
          workspaceId: LIVE_GROUP_ID,
          workspaceName: "Živé relace",
        }));
      if (orphans.length) {
        groups.push({ id: LIVE_GROUP_ID, name: "Živé relace", path: "", tabs: orphans });
        for (const t of orphans) {
          if (!statuses.has(t.ptyId)) statuses.set(t.ptyId, "idle");
          watchTabStatus(t.ptyId);
        }
      }

      workspaces.value = groups;
    } catch (e: any) {
      listError.value = e?.message ?? "Failed to load sessions";
    } finally {
      loading.value = false;
    }
  }

  function chatFor(id: number) {
    return chats.value.find((chat) => chat.id === id);
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
  function applyEvent(chat: RemoteChat, event: Record<string, any>) {
    switch (event.type) {
      case "text.delta":
        appendRemoteText(chat, "assistant", event.text ?? "");
        return;
      case "thinking.delta":
        appendRemoteText(chat, "thinking", event.text ?? "");
        return;
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
        chat.busy = false;
        chat.messages.forEach((message) => { message.partial = false; });
        const watching = view.value === "chat" && activeChat.value?.id === chat.id;
        if (!watching) chat.unseen = event.type === "turn.failed" ? "error" : "review";
        return;
      }
      case "session.id":
        if (typeof event.sessionId === "string") chat.claudeSessionId = event.sessionId;
        return;
    }
  }

  function safeJson(raw: string): unknown {
    try { return JSON.parse(raw); } catch { return null; }
  }

  function watchChat(chat: RemoteChat) {
    client?.subscribe(`chat-event-${chat.id}`, (payload) => {
      const batch = (typeof payload === "string" ? safeJson(payload) : payload) as
        { events?: Array<Record<string, any>> } | null;
      for (const event of batch?.events ?? []) applyEvent(chat, event);
    });
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

  // Narrow reader: only recognizes a can_use_tool control_request (Claude), a
  // Codex requestApproval, or a generic session/request_permission (ACP) —
  // everything else on this raw channel is ignored on purpose (see spec's
  // "Non-goals": no full protocol parsing on mobile, only enough to unblock
  // a turn).
  function watchChatPermissions(chat: RemoteChat) {
    const rawEvent = chat.transport === "claude-cli" ? `claude-data-${chat.id}` : `acp-req-${chat.id}`;
    client?.subscribe(rawEvent, (payload) => {
      const line = typeof payload === "string" ? safeJson(payload) : payload;
      if (!line || typeof line !== "object") return;
      const msg = line as Record<string, any>;

      if (chat.transport === "claude-cli") {
        if (msg.type !== "control_request" || msg.request?.subtype !== "can_use_tool") return;
        const input = msg.request.input ?? {};
        const detail = input.command ?? input.file_path ?? input.path ?? "";
        chat.pendingPermission = {
          requestId: msg.request_id,
          toolName: msg.request.tool_name ?? "Tool",
          detail: String(detail),
          options: [],
        };
        return;
      }

      // ACP: server->client REQUEST has both method and id.
      if (typeof msg.id !== "number" || !msg.method) return;

      if (CODEX_APPROVAL_METHODS.includes(msg.method)) {
        const p = msg.params ?? {};
        const command = typeof p.command === "string" ? p.command : "";
        const toolName = msg.method.includes("commandExecution") ? "Run command"
          : msg.method.includes("fileChange") ? "Apply file changes"
          : "Grant additional permission";
        chat.pendingPermission = {
          rpcId: msg.id,
          toolName,
          detail: command,
          options: CODEX_APPROVAL_OPTIONS,
        };
        return;
      }

      if (msg.method !== "session/request_permission") return;
      const options = (msg.params?.options ?? []).map((o: any) => ({ optionId: o.optionId, kind: o.kind }));
      chat.pendingPermission = {
        rpcId: msg.id,
        toolName: msg.params?.toolCall?.title ?? msg.params?.title ?? "Tool",
        detail: "",
        options,
      };
    });
  }

  async function loadChats() {
    if (!client) return;
    try {
      const next = await client.call("remote_list_chats") as RemoteChat[];
      chats.value = next.map((chat) => ({ ...chat, messages: Array.isArray(chat.messages) ? chat.messages : [] }));
      for (const chat of chats.value) { watchChat(chat); watchChatPermissions(chat); }
      client.subscribe("remote-chats", (payload) => {
        const change = typeof payload === "string" ? safeJson(payload) : payload;
        const incoming = (change as any)?.chat as RemoteChat | undefined;
        if (!incoming) return;
        const existing = chatFor(incoming.id);
        if (existing) Object.assign(existing, incoming);
        else { chats.value.push(incoming); watchChat(incoming); watchChatPermissions(incoming); }
      });
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
    view.value = "dashboard";
  }

  async function sendChat(text: string) {
    const chat = activeChat.value;
    if (!client || !chat || !text.trim() || chat.busy) return;
    chat.messages.push({ id: Date.now(), role: "user", text: text.trim() });
    chat.busy = true;
    try {
      if (chat.transport === "claude-cli") {
        // Go's wsArgs.ID is string-typed — an un-stringified numeric id
        // silently fails to decode and the call hangs with no reply (same
        // bug class Task 1 fixed for pty ids).
        await client.call("claude_send", { id: String(chat.id), text: text.trim(), sessionId: chat.claudeSessionId || null });
      } else {
        await client.call("acp_send", { id: String(chat.id), text: text.trim() });
      }
    } catch (e: any) {
      chat.busy = false;
      chat.messages.push({ id: Date.now() + 1, role: "assistant", text: `Chyba odeslání: ${e?.message ?? e}` });
    }
  }

  async function createChat(workspaceId: number, agentKind: "codex" | "claude") {
    if (!client) throw new Error("not connected");
    const chat = await client.call("remote_create_chat", { workspaceId, agentKind }) as RemoteChat;
    chats.value.push(chat);
    watchChat(chat);
    watchChatPermissions(chat);
    openChat(chat);
  }

  async function respondChatPermission(chatId: number, allow: boolean) {
    const chat = chatFor(chatId);
    const pending = chat?.pendingPermission;
    if (!client || !chat || !pending) return;
    chat.pendingPermission = null;
    try {
      if (pending.requestId) {
        await client.call("claude_respond_control", {
          id: String(chat.id),
          requestId: pending.requestId,
          response: allow
            ? { behavior: "allow", updatedInput: {} }
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
        await client.call("acp_respond_permission", {
          id: String(chat.id),
          rpcId: pending.rpcId,
          optionId,
        });
      }
    } catch (e: any) {
      chat.messages.push({ id: Date.now(), role: "assistant", text: `Odpověď na povolení selhala: ${e?.message ?? e}` });
    }
  }

  function openTerminal(tab: Tab) {
    activeTab.value = tab;
    view.value = "terminal";
    markTabSeen(tab.ptyId);
  }

  function markTabSeen(ptyId: number) {
    const s = statuses.get(ptyId);
    if (s === "review" || s === "error") statuses.set(ptyId, "idle");
  }

  function showDashboard() {
    view.value = "dashboard";
  }

  function showSessions() {
    view.value = "sessions";
  }

  function showChats() { view.value = "chats"; }

  function closeTerminal() {
    if (activeTab.value) {
      client?.unsubscribe(`pty-data-${activeTab.value.ptyId}`);
    }
    activeTab.value = null;
    view.value = "dashboard";
  }

  function getClient(): BurrowWsClient {
    if (!client) throw new Error("not connected");
    return client;
  }

  return {
    baseUrl, token, connected, connecting, connectError,
    view, workspaces, loading, listError, activeTab,
    chats, activeChat,
    pair, connect, disconnect, loadSessions, loadChats, openTerminal, closeTerminal, showDashboard, showSessions, showChats, openChat, closeChat, sendChat, createChat,
    statusFor, getClient,
    markTabSeen, markChatSeen, chatStatus, respondChatPermission, reconnecting,
  };
});
