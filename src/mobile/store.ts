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

export interface WorkspaceGroup {
  id: number;
  name: string;
  path: string;
  tabs: Tab[];
}

export type View = "connect" | "sessions" | "terminal";

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
        if (view.value === "terminal") view.value = "sessions";
      };
      client = c;
      connected.value = true;
      baseUrl.value = normalized;
      token.value = tok;
      setConfig(URL_CONFIG_KEY, normalized);
      setConfig(TOKEN_CONFIG_KEY, tok);
      view.value = "sessions";
      await loadSessions();
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
        const tabs: any[] = await client.call("list_terminal_tabs", { workspaceId: ws.id });
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
      workspaces.value = groups;
    } catch (e: any) {
      listError.value = e?.message ?? "Failed to load sessions";
    } finally {
      loading.value = false;
    }
  }

  function openTerminal(tab: Tab) {
    activeTab.value = tab;
    view.value = "terminal";
  }

  function closeTerminal() {
    if (activeTab.value) {
      client?.unsubscribe(`pty-data-${activeTab.value.ptyId}`);
    }
    activeTab.value = null;
    view.value = "sessions";
  }

  function getClient(): BurrowWsClient {
    if (!client) throw new Error("not connected");
    return client;
  }

  return {
    baseUrl, token, connected, connecting, connectError,
    view, workspaces, loading, listError, activeTab,
    connect, disconnect, loadSessions, openTerminal, closeTerminal,
    statusFor, getClient,
  };
});
