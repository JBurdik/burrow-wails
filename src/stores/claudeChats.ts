import { ref, computed } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { createActor } from "xstate";
import type { TermStatus } from "@/lib/terminalStatus";
import { agentStatusMachine } from "@/machines/agentStatus";
import type { AgentStatusEvent } from "@/machines/agentStatus";
import { useProvidersStore, chatTransportFor, type ChatTransport } from "@/stores/providers";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

export interface ClaudeSession {
  id: number;
  workspaceId: number;
  claudeSessionId: string; // captured from stream-json system/init
  title: string;
  busy: boolean;
  messageCount: number;
  // Mirrors the terminal-tab status model so chats show the same dots/bell in the
  // Sidebar. "permission" = blocked on an allow/deny decision (amber + bell).
  status?: TermStatus;
  // The hidden per-repo Manager (Mission Control) session — kept out of the
  // Sidebar chat list so it isn't a duplicate of the floating Manager card.
  control?: boolean;
  // Set when the user manually renames the tab — prevents auto-title from overwriting.
  pinnedTitle?: boolean;
  // Which agent backs this chat — a chatAgents store id (default 'claude').
  agentKind?: string;
  // Native provider runtime or a generic Agent Client Protocol adapter.
  transport?: ChatTransport;
  // Set when the chat was archived (soft-hidden, process stopped, reversible via
  // unarchive). null/undefined means it's a normal, listed session.
  archivedAt?: number | null;
  // Manual override of the computed "settled" (no attention needed) bucket —
  // "settled" pins it settled even mid-run, "active" pins it active even once
  // idle. Cleared back to auto (undefined) whenever a new turn starts (see sync()).
  settledOverride?: "settled" | "active" | null;
  // Last time anything happened on this chat (message, status/title change) —
  // the reference point for the days-of-inactivity auto-settle threshold.
  lastActivityAt?: number;
}

const SESSIONS_KEY = "chatSessions";
const SESSIONS_LEGACY_KEY = "burrow.claude.sessions";
const ACTIVE_KEY = "chatActiveByWs";
const ACTIVE_LEGACY_KEY = "burrow.claude.active";
const COUNTER_KEY = "chatIdCounter";
const COUNTER_LEGACY_KEY = "burrow.claude.nextId";
const TURNS_KEY = "chatTurns";
const TURNS_LEGACY_KEY = "burrow.claude.turns";
const RULES_KEY = "chatPermissionRules";
const RULES_LEGACY_KEY = "burrow.claude.permRules";

export interface TurnEvent {
  ts: number;
  inputTokens: number;
  outputTokens: number;
}

const WINDOW_MS = 5 * 60 * 60 * 1000; // 5 hours

type SessionActor = ReturnType<typeof createActor<typeof agentStatusMachine>>;

export const useClaudeChatsStore = defineStore("claudeChats", () => {
  const sessions = ref<ClaudeSession[]>([]);
  const activeByWs = ref<Record<number, number>>({});
  let nextId = 1;
  const turns = ref<TurnEvent[]>([]);
  // "Allow always" rules — opaque match keys (e.g. "Bash:git" or "Write").
  // Matched against the key(s) derived from an incoming can_use_tool request.
  const permissionRules = ref<string[]>([]);

  // XState actors — one per session, keyed by session id. Not persisted.
  const actors = new Map<number, SessionActor>();

  function spawnActor(session: ClaudeSession): SessionActor {
    const actor = createActor(agentStatusMachine, { input: {} }).start();
    actor.subscribe((snapshot) => {
      session.status = snapshot.value as TermStatus;
    });
    actors.set(session.id, actor);
    return actor;
  }

  configReady.then(() => {
    migrateFromLocalStorage(SESSIONS_LEGACY_KEY, SESSIONS_KEY);
    sessions.value = getConfig<ClaudeSession[]>(SESSIONS_KEY, []);
    // Restore actors for sessions loaded from config (all start idle — correct since busy=false on persist).
    sessions.value.forEach(spawnActor);

    migrateFromLocalStorage(ACTIVE_LEGACY_KEY, ACTIVE_KEY);
    activeByWs.value = getConfig<Record<number, number>>(ACTIVE_KEY, {});

    migrateFromLocalStorage(COUNTER_LEGACY_KEY, COUNTER_KEY);
    nextId = getConfig<number>(COUNTER_KEY, 1);

    migrateFromLocalStorage(TURNS_LEGACY_KEY, TURNS_KEY);
    turns.value = getConfig<TurnEvent[]>(TURNS_KEY, []);

    migrateFromLocalStorage(RULES_LEGACY_KEY, RULES_KEY);
    permissionRules.value = getConfig<string[]>(RULES_KEY, []);
  });

  function addPermissionRule(key: string) {
    if (!key || permissionRules.value.includes(key)) return;
    permissionRules.value.push(key);
    setConfig(RULES_KEY, permissionRules.value);
  }
  function hasPermissionRule(keys: string[]): boolean {
    return keys.some((k) => permissionRules.value.includes(k));
  }
  function clearPermissionRules() {
    permissionRules.value = [];
    setConfig(RULES_KEY, []);
  }

  function persist() {
    const toSave = sessions.value.map((s) => ({ ...s, busy: false }));
    setConfig(SESSIONS_KEY, toSave);
    setConfig(ACTIVE_KEY, activeByWs.value);
    setConfig(COUNTER_KEY, nextId);
  }

  function sessionsForWs(workspaceId: number): ClaudeSession[] {
    return sessions.value.filter((s) => s.workspaceId === workspaceId && !s.archivedAt);
  }

  function archivedSessionsForWs(workspaceId: number): ClaudeSession[] {
    return sessions.value
      .filter((s) => s.workspaceId === workspaceId && !!s.archivedAt)
      .sort((a, b) => (b.archivedAt ?? 0) - (a.archivedAt ?? 0));
  }

  function activeSession(workspaceId: number): ClaudeSession | undefined {
    const activeId = activeByWs.value[workspaceId];
    return sessions.value.find((s) => s.id === activeId && s.workspaceId === workspaceId);
  }

  // Create and activate a new session for this workspace.
  function create(workspaceId: number, opts?: { agentKind?: string }): ClaudeSession {
    const id = nextId++;
    const agentKind = opts?.agentKind ?? 'claude';
    const transport: ChatTransport =
      (() => { const a = useProvidersStore().byId(agentKind); return a ? chatTransportFor(a) : (agentKind === 'claude' ? 'claude-cli' : 'acp'); })();
    const session: ClaudeSession = {
      id,
      workspaceId,
      claudeSessionId: "",
      title: `Chat ${sessionsForWs(workspaceId).length + 1}`,
      busy: false,
      messageCount: 0,
      agentKind,
      transport,
      lastActivityAt: Date.now(),
    };
    sessions.value.push(session);
    // Pass the REACTIVE array element (not the raw `session`) so the actor's
    // status mutations go through Vue's proxy and actually trigger reactivity.
    spawnActor(sessions.value[sessions.value.length - 1]);
    activeByWs.value[workspaceId] = id;
    persist();
    return session;
  }

  // Ensure at least one session exists for this workspace; return active.
  function ensureSession(workspaceId: number): ClaudeSession {
    const existing = sessionsForWs(workspaceId);
    if (existing.length === 0) return create(workspaceId);
    const active = activeSession(workspaceId);
    if (active) return active;
    activeByWs.value[workspaceId] = existing[0].id;
    persist();
    return existing[0];
  }

  function setActive(workspaceId: number, sessionId: number) {
    activeByWs.value[workspaceId] = sessionId;
    persist();
  }

  async function remove(id: number) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    actors.get(id)?.stop();
    actors.delete(id);
    await invoke(s.transport === "claude-cli" ? "claude_stop" : s.transport === "codex-app-server" ? "codex_stop" : "acp_stop", { id }).catch(() => {});
    sessions.value = sessions.value.filter((x) => x.id !== id);
    // If removed was active, fall back to first remaining for that ws.
    if (activeByWs.value[s.workspaceId] === id) {
      const remaining = sessionsForWs(s.workspaceId);
      if (remaining.length) activeByWs.value[s.workspaceId] = remaining[0].id;
      else delete activeByWs.value[s.workspaceId];
    }
    persist();
  }

  // Soft-hide: stop the process like remove(), but keep the row (and its
  // history) so it can be found again in the Archived shelf and unarchived.
  async function archive(id: number) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    actors.get(id)?.stop();
    actors.delete(id);
    await invoke(s.transport === "claude-cli" ? "claude_stop" : s.transport === "codex-app-server" ? "codex_stop" : "acp_stop", { id }).catch(() => {});
    s.archivedAt = Date.now();
    if (activeByWs.value[s.workspaceId] === id) {
      const remaining = sessionsForWs(s.workspaceId);
      if (remaining.length) activeByWs.value[s.workspaceId] = remaining[0].id;
      else delete activeByWs.value[s.workspaceId];
    }
    persist();
  }

  // Reverses archive(); does NOT restart the actor/process — the caller (chat
  // reopen) is responsible for that, same as opening any other existing chat.
  function unarchive(id: number) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    s.archivedAt = null;
    persist();
  }

  function settle(id: number) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    s.settledOverride = "settled";
    persist();
  }

  function unsettle(id: number) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    s.settledOverride = "active";
    persist();
  }

  // t3code's own default for "Days of inactivity before auto-settle"
  // (`sf.sidebarAutoSettleAfterDays ?? 3` in its settings panel).
  const AUTO_SETTLE_AFTER_DAYS = 3;
  const DAY_MS = 24 * 60 * 60 * 1000;

  // Whether a chat needs no more attention right now. Ported from t3code's
  // settle decision: pending work always wins (not settled), then a manual
  // pin, then — only past AUTO_SETTLE_AFTER_DAYS of inactivity — auto-settled.
  // A chat that just finished is NOT settled yet; it ages into the shelf.
  function isSettled(s: ClaudeSession | undefined): boolean {
    if (!s) return false;
    if (s.busy || s.status === "running" || s.status === "waiting" || s.status === "permission") return false;
    if (s.settledOverride === "settled") return true;
    if (s.settledOverride === "active") return false;
    const last = s.lastActivityAt ?? 0;
    return Date.now() - last >= AUTO_SETTLE_AFTER_DAYS * DAY_MS;
  }

  // Turn event tracking for 5-hour usage window.
  function recordTurn(inputTokens: number, outputTokens: number) {
    const now = Date.now();
    turns.value.push({ ts: now, inputTokens, outputTokens });
    // Prune events older than 5h to keep storage small.
    turns.value = turns.value.filter((t) => now - t.ts < WINDOW_MS);
    setConfig(TURNS_KEY, turns.value);
  }

  const turnsInWindow = computed(() => {
    const now = Date.now();
    return turns.value.filter((t) => now - t.ts < WINDOW_MS);
  });

  const windowTokens = computed(() => {
    return turnsInWindow.value.reduce((acc, t) => acc + t.inputTokens + t.outputTokens, 0);
  });

  // Earliest turn in window — resets when no turns remain.
  const windowStart = computed(() => {
    const wt = turnsInWindow.value;
    return wt.length ? wt[0].ts : null;
  });

  // Called by ClaudeChat.vue to sync live state back.
  function sync(id: number, patch: Partial<Pick<ClaudeSession, "busy" | "messageCount" | "claudeSessionId" | "title" | "status" | "control" | "agentKind" | "transport">>) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s) return;
    // A fresh turn starting is real reactivation — drop any settle/unsettle pin
    // so the next done/review transition auto-settles again instead of being
    // stuck (mirrors t3code clearing settledOverride on a system-triggered unsettle).
    if (patch.status === "running" && s.settledOverride) s.settledOverride = null;
    s.lastActivityAt = Date.now();
    Object.assign(s, patch);
    if (patch.claudeSessionId !== undefined || patch.title !== undefined || patch.messageCount !== undefined || patch.control !== undefined || patch.agentKind !== undefined || patch.transport !== undefined) {
      persist();
    }
  }

  function sendStatusEvent(id: number, event: AgentStatusEvent) {
    actors.get(id)?.send(event);
  }

  function markSeen(id: number) {
    actors.get(id)?.send({ type: "MARK_SEEN" });
  }

  // Sessions whose workspace is currently in ws.opened — used by App.vue for keep-alive mounting.
  // The caller filters by opened workspace ids.
  const allSessions = computed(() => sessions.value);

  return {
    sessions,
    activeByWs,
    allSessions,
    turns,
    turnsInWindow,
    windowTokens,
    windowStart,
    recordTurn,
    sessionsForWs,
    archivedSessionsForWs,
    activeSession,
    create,
    ensureSession,
    setActive,
    remove,
    archive,
    unarchive,
    settle,
    unsettle,
    isSettled,
    sync,
    permissionRules,
    addPermissionRule,
    hasPermissionRule,
    clearPermissionRules,
    sendStatusEvent,
    markSeen,
  };
});
