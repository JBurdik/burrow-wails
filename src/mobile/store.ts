import { defineStore } from "pinia";
import { reactive, ref } from "vue";
import { BurrowWsClient } from "./api";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

const URL_LEGACY_KEY = "burrow-mobile-url";
const TOKEN_LEGACY_KEY = "burrow-mobile-token";
const URL_CONFIG_KEY = "mobileBaseUrl";
const TOKEN_CONFIG_KEY = "mobileToken";

export type TabStatus = "idle" | "running" | "waiting" | "permission" | "done";

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

  let client: BurrowWsClient | null = null;
  const doneTimers = new Map<number, number>();

  function statusFor(ptyId: number): TabStatus {
    return statuses.get(ptyId) ?? "idle";
  }

  function watchTabStatus(ptyId: number) {
    client?.subscribe(`pty-hook-${ptyId}`, (payload) => {
      // Broadcast branch only ever sends the bare state string (see api.ts note).
      const state = typeof payload === "string" ? payload : payload?.state;
      if (state === "running" || state === "waiting" || state === "permission") {
        const t = doneTimers.get(ptyId);
        if (t !== undefined) { window.clearTimeout(t); doneTimers.delete(ptyId); }
        statuses.set(ptyId, state);
      } else if (state === "done") {
        statuses.set(ptyId, "done");
        const t = window.setTimeout(() => statuses.set(ptyId, "idle"), 4000);
        doneTimers.set(ptyId, t);
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
      };
      client = c;
      connected.value = true;
      baseUrl.value = normalized;
      token.value = tok;
      setConfig(URL_CONFIG_KEY, normalized);
      setConfig(TOKEN_CONFIG_KEY, tok);
      view.value = "dashboard";
      await Promise.all([loadSessions(), loadChats()]);
    } catch (e: any) {
      connectError.value = e?.message ?? "Connection failed";
      connected.value = false;
      throw e;
    } finally {
      connecting.value = false;
    }
  }

  function disconnect() {
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

  function handleClaudeData(chat: RemoteChat, raw: unknown) {
    const event = typeof raw === "string" ? safeJson(raw) : raw;
    if (!event || typeof event !== "object") return;
    const data = event as Record<string, any>;
    if (data.type === "assistant") {
      for (const block of data.message?.content ?? []) {
        if (block.type === "text") appendRemoteText(chat, "assistant", block.text ?? "");
        if (block.type === "thinking") appendRemoteText(chat, "thinking", block.thinking ?? "");
        if (block.type === "tool_use") chat.messages.push({ id: Date.now() + chat.messages.length, role: "tool", text: block.name ?? "Tool", toolInput: block.input ?? {} });
      }
    }
    if (data.type === "result" || data.type === "exit") {
      chat.busy = false;
      chat.messages.forEach((message) => { message.partial = false; });
    }
  }

  function handleAcpData(chat: RemoteChat, raw: unknown) {
    const event = typeof raw === "string" ? safeJson(raw) : raw;
    if (!event || typeof event !== "object") return;
    const data = event as Record<string, any>;
    if (data._burrow === "session" && typeof data.sessionId === "string") chat.claudeSessionId = data.sessionId;
    if (data._burrow === "exit" || ("id" in data && !("method" in data))) {
      chat.busy = false;
      chat.messages.forEach((message) => { message.partial = false; });
    }
    const update = data.params?.update;
    if (data.method !== "session/update" || !update) return;
    const text = update.content?.text ?? "";
    if (update.sessionUpdate === "agent_message_chunk") appendRemoteText(chat, "assistant", text);
    if (update.sessionUpdate === "agent_thought_chunk") appendRemoteText(chat, "thinking", text);
  }

  function safeJson(raw: string): unknown {
    try { return JSON.parse(raw); } catch { return null; }
  }

  function watchChat(chat: RemoteChat) {
    client?.subscribe(`claude-data-${chat.id}`, (payload) => handleClaudeData(chat, payload));
    client?.subscribe(`acp-data-${chat.id}`, (payload) => handleAcpData(chat, payload));
  }

  async function loadChats() {
    if (!client) return;
    try {
      const next = await client.call("remote_list_chats") as RemoteChat[];
      chats.value = next.map((chat) => ({ ...chat, messages: Array.isArray(chat.messages) ? chat.messages : [] }));
      for (const chat of chats.value) watchChat(chat);
      client.subscribe("remote-chats", (payload) => {
        const change = typeof payload === "string" ? safeJson(payload) : payload;
        const incoming = (change as any)?.chat as RemoteChat | undefined;
        if (!incoming) return;
        const existing = chatFor(incoming.id);
        if (existing) Object.assign(existing, incoming);
        else { chats.value.push(incoming); watchChat(incoming); }
      });
    } catch (e: any) {
      listError.value = e?.message ?? "Failed to load chats";
    }
  }

  function openChat(chat: RemoteChat) {
    activeChat.value = chat;
    view.value = "chat";
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
        await client.call("claude_send", { id: chat.id, text: text.trim(), sessionId: chat.claudeSessionId || null });
      } else {
        await client.call("acp_send", { id: chat.id, text: text.trim() });
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
    openChat(chat);
  }

  function openTerminal(tab: Tab) {
    activeTab.value = tab;
    view.value = "terminal";
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
  };
});
