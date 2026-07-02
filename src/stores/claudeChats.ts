import { ref, computed } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { createActor } from "xstate";
import type { TermStatus } from "@/lib/terminalStatus";
import { agentStatusMachine } from "@/machines/agentStatus";
import type { AgentStatusEvent } from "@/machines/agentStatus";
import { useChatAgentsStore } from "@/stores/chatAgents";

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
  // Wire protocol: 'stream-json' (Claude CLI) or 'acp' (Agent Client Protocol).
  transport?: 'stream-json' | 'acp';
}

const SESSIONS_KEY = "burrow.claude.sessions";
const ACTIVE_KEY = "burrow.claude.active";
const COUNTER_KEY = "burrow.claude.nextId";
const TURNS_KEY = "burrow.claude.turns";
const RULES_KEY = "burrow.claude.permRules";

export interface TurnEvent {
  ts: number;
  inputTokens: number;
  outputTokens: number;
}

const WINDOW_MS = 5 * 60 * 60 * 1000; // 5 hours

function loadTurns(): TurnEvent[] {
  try {
    const raw = localStorage.getItem(TURNS_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch { return []; }
}

function loadSessions(): ClaudeSession[] {
  try {
    const raw = localStorage.getItem(SESSIONS_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch { return []; }
}

function loadActive(): Record<number, number> {
  try {
    const raw = localStorage.getItem(ACTIVE_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch { return {}; }
}

function loadCounter(): number {
  return parseInt(localStorage.getItem(COUNTER_KEY) ?? "1", 10);
}

function loadRules(): string[] {
  try {
    const raw = localStorage.getItem(RULES_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch { return []; }
}

type SessionActor = ReturnType<typeof createActor<typeof agentStatusMachine>>;

export const useClaudeChatsStore = defineStore("claudeChats", () => {
  const sessions = ref<ClaudeSession[]>(loadSessions());
  const activeByWs = ref<Record<number, number>>(loadActive());
  let nextId = loadCounter();
  const turns = ref<TurnEvent[]>(loadTurns());
  // "Allow always" rules — opaque match keys (e.g. "Bash:git" or "Write").
  // Matched against the key(s) derived from an incoming can_use_tool request.
  const permissionRules = ref<string[]>(loadRules());

  // XState actors — one per session, keyed by session id. Not persisted.
  const actors = new Map<number, SessionActor>();

  function spawnActor(session: ClaudeSession): SessionActor {
    const actor = createActor(agentStatusMachine).start();
    actor.subscribe((snapshot) => {
      session.status = snapshot.value as TermStatus;
    });
    actors.set(session.id, actor);
    return actor;
  }

  // Restore actors for sessions loaded from localStorage (all start idle — correct since busy=false on persist).
  sessions.value.forEach(spawnActor);

  function addPermissionRule(key: string) {
    if (!key || permissionRules.value.includes(key)) return;
    permissionRules.value.push(key);
    localStorage.setItem(RULES_KEY, JSON.stringify(permissionRules.value));
  }
  function hasPermissionRule(keys: string[]): boolean {
    return keys.some((k) => permissionRules.value.includes(k));
  }
  function clearPermissionRules() {
    permissionRules.value = [];
    localStorage.removeItem(RULES_KEY);
  }

  function persist() {
    const toSave = sessions.value.map((s) => ({ ...s, busy: false }));
    localStorage.setItem(SESSIONS_KEY, JSON.stringify(toSave));
    localStorage.setItem(ACTIVE_KEY, JSON.stringify(activeByWs.value));
    localStorage.setItem(COUNTER_KEY, String(nextId));
  }

  function sessionsForWs(workspaceId: number): ClaudeSession[] {
    return sessions.value.filter((s) => s.workspaceId === workspaceId);
  }

  function activeSession(workspaceId: number): ClaudeSession | undefined {
    const activeId = activeByWs.value[workspaceId];
    return sessions.value.find((s) => s.id === activeId && s.workspaceId === workspaceId);
  }

  // Create and activate a new session for this workspace.
  function create(workspaceId: number, opts?: { agentKind?: string }): ClaudeSession {
    const id = nextId++;
    const agentKind = opts?.agentKind ?? 'claude';
    const transport: 'stream-json' | 'acp' =
      useChatAgentsStore().byId(agentKind)?.transport ?? (agentKind === 'claude' ? 'stream-json' : 'acp');
    const session: ClaudeSession = {
      id,
      workspaceId,
      claudeSessionId: "",
      title: `Chat ${sessionsForWs(workspaceId).length + 1}`,
      busy: false,
      messageCount: 0,
      agentKind,
      transport,
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
    await invoke(s.transport === "acp" ? "acp_stop" : "claude_stop", { id }).catch(() => {});
    sessions.value = sessions.value.filter((x) => x.id !== id);
    // If removed was active, fall back to first remaining for that ws.
    if (activeByWs.value[s.workspaceId] === id) {
      const remaining = sessionsForWs(s.workspaceId);
      if (remaining.length) activeByWs.value[s.workspaceId] = remaining[0].id;
      else delete activeByWs.value[s.workspaceId];
    }
    persist();
  }

  // Turn event tracking for 5-hour usage window.
  function recordTurn(inputTokens: number, outputTokens: number) {
    const now = Date.now();
    turns.value.push({ ts: now, inputTokens, outputTokens });
    // Prune events older than 5h to keep storage small.
    turns.value = turns.value.filter((t) => now - t.ts < WINDOW_MS);
    localStorage.setItem(TURNS_KEY, JSON.stringify(turns.value));
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
    activeSession,
    create,
    ensureSession,
    setActive,
    remove,
    sync,
    permissionRules,
    addPermissionRule,
    hasPermissionRule,
    clearPermissionRules,
    sendStatusEvent,
    markSeen,
  };
});
