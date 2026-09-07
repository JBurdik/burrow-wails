import { ref, computed } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { createActor } from "xstate";
import type { TermStatus } from "@/lib/terminalStatus";
import { agentStatusMachine } from "@/machines/agentStatus";
import type { AgentStatusEvent } from "@/machines/agentStatus";
import { useProvidersStore, chatTransportFor, type ChatTransport } from "@/stores/providers";
import { useWorkspaceStore } from "@/stores/workspace";
import { useGitStore } from "@/stores/git";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import { listen } from "@tauri-apps/api/event";
import { forgetChatSettings } from "@/lib/chatSettings";
import { dropChatSession } from "@/lib/chatSession";

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
  // Model id this chat is currently running (mirrored from the composer's
  // picker) — badged in the Sidebar next to the agent icon.
  model?: string;
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
  // Branch checked out when the chat was created — a snapshot, not live.
  branch?: string;
}

/**
 * The chat LIST lives in SQLite and Go owns it (src-wails/chats.go). There is
 * no `chatSessions` key and no `chatIdCounter` any more: both clients were
 * rewriting the whole of config.json, so the desktop saving a font preference
 * could revert a chat the phone had just created — and the reverted counter
 * then handed the next desktop chat the phone's id and its running CLI.
 *
 * `chatActiveByWs` deliberately STAYS in config.json. Which chat is selected
 * is per-device, exactly like the seen-at receipts: the desktop being on chat
 * 78 says nothing about what the phone should be showing.
 */
const ACTIVE_KEY = "chatActiveByWs";
const ACTIVE_LEGACY_KEY = "burrow.claude.active";

/** Wire shape of src-wails/chats.go's Chat. */
interface ChatRow {
  id: number;
  workspace_id: number;
  title: string;
  pinned_title: boolean;
  claude_session_id: string;
  message_count: number;
  control: boolean;
  agent_kind: string;
  transport: string;
  model: string;
  branch: string;
  settled_override: string;
  archived_at: number;
  last_activity_at: number;
}

// The one place the column names and the client's field names meet. `busy`
// and `status` are absent from the row on purpose — busy was always persisted
// as false, and the status IS the phase.
function sessionFromRow(r: ChatRow): ClaudeSession {
  return {
    id: r.id,
    workspaceId: r.workspace_id,
    title: r.title,
    pinnedTitle: r.pinned_title || undefined,
    claudeSessionId: r.claude_session_id,
    messageCount: r.message_count,
    control: r.control || undefined,
    agentKind: r.agent_kind || undefined,
    transport: (r.transport || undefined) as ChatTransport | undefined,
    model: r.model || undefined,
    branch: r.branch || undefined,
    settledOverride: (r.settled_override || null) as ClaudeSession["settledOverride"],
    archivedAt: r.archived_at || null,
    lastActivityAt: r.last_activity_at || undefined,
    busy: false,
  };
}

function rowFromSession(s: ClaudeSession): ChatRow {
  return {
    id: s.id,
    workspace_id: s.workspaceId,
    title: s.title ?? "",
    pinned_title: !!s.pinnedTitle,
    claude_session_id: s.claudeSessionId ?? "",
    message_count: s.messageCount ?? 0,
    control: !!s.control,
    agent_kind: s.agentKind ?? "",
    transport: s.transport ?? "",
    model: s.model ?? "",
    branch: s.branch ?? "",
    settled_override: s.settledOverride ?? "",
    archived_at: s.archivedAt ?? 0,
    last_activity_at: s.lastActivityAt ?? 0,
  };
}
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
  const turns = ref<TurnEvent[]>([]);
  // "Allow always" rules — opaque match keys (e.g. "Bash:git" or "Write").
  // Matched against the key(s) derived from an incoming can_use_tool request.
  const permissionRules = ref<string[]>([]);

  // XState actors — one per session, keyed by session id. Not persisted.
  const actors = new Map<number, SessionActor>();

  function spawnActor(session: ClaudeSession): SessionActor {
    const actor = createActor(agentStatusMachine, { input: {} }).start();
    // The actor's own state is authoritative from here on. Adopt it
    // immediately: `status` is persisted with the session, so an app closed
    // mid-turn comes back claiming `running` with no process behind it, and
    // subscribe() only fires on later transitions — leaving that stale dot
    // spinning forever.
    session.status = actor.getSnapshot().value as TermStatus;
    actor.subscribe((snapshot) => {
      session.status = snapshot.value as TermStatus;
    });
    actors.set(session.id, actor);
    return actor;
  }

  /**
   * Load (or re-load) the shared chat list.
   *
   * MERGES rather than replaces: a re-load triggered by `chats-changed` must
   * not discard a running actor or an in-flight `busy` for a chat that is
   * only being re-read. Rows the server no longer has are dropped, with their
   * actors, because that is what a delete on the other client looks like from
   * here.
   */
  async function reload() {
    let rows: ChatRow[];
    try {
      rows = await invoke<ChatRow[]>("list_chats");
    } catch {
      return; // no backend (browser-only dev) — leave whatever is loaded
    }
    const incoming = new Map(rows.map((r) => [r.id, r]));

    for (const [id, actor] of actors) {
      if (!incoming.has(id)) {
        actor.stop();
        actors.delete(id);
      }
    }

    const next: ClaudeSession[] = [];
    for (const row of rows) {
      const existing = sessions.value.find((s) => s.id === row.id);
      if (existing) {
        // Keep the live-only fields this store owns; take the rest from the
        // row, which is the shared truth.
        const { busy, status } = existing;
        Object.assign(existing, sessionFromRow(row), { busy, status });
        next.push(existing);
      } else {
        next.push(sessionFromRow(row));
      }
    }
    sessions.value = next;
    // ONLY for sessions that do not have an actor yet. Spawning
    // unconditionally re-created every actor on every reload, which reset
    // every chat's status to idle and orphaned the previous actor without
    // stopping it — and since a reload now happens on every `chats-changed`,
    // that meant every chat's dot went blank whenever anything anywhere
    // touched a chat.
    for (const s of sessions.value) {
      if (!actors.has(s.id)) spawnActor(s);
    }
  }

  // Writes this client has in flight. A `chats-changed` while one is
  // outstanding is our OWN echo: reloading on it would be a round trip per
  // save, and sync() saves on every message a streaming turn produces.
  let selfWrites = 0;

  void reload();
  // Both clients follow this, so a chat created anywhere appears everywhere
  // without a reload — which is the whole reason the list moved out of a file
  // each client rewrote wholesale.
  void listen("chats-changed", () => {
    if (selfWrites > 0) return;
    void reload();
  });

  configReady.then(() => {
    migrateFromLocalStorage(ACTIVE_LEGACY_KEY, ACTIVE_KEY);
    activeByWs.value = getConfig<Record<number, number>>(ACTIVE_KEY, {});

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

  /**
   * Write the chat rows this client holds, plus the per-device selection.
   *
   * `save_chats` is an upsert that never deletes, so this cannot remove a
   * chat another client created between our last read and this write — the
   * failure that made a phone-created chat vanish. Deletion is explicit
   * (`delete_chat`, from remove()).
   */
  function persist() {
    selfWrites++;
    void invoke("save_chats", { chats: sessions.value.map(rowFromSession) })
      .catch(() => {
        // Best effort, same contract setConfig had. The next persist retries
        // with the latest state, and `chats-changed` re-reads either way.
      })
      .finally(() => {
        selfWrites--;
      });
    setConfig(ACTIVE_KEY, activeByWs.value);
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
  /**
   * Create a chat. Async because the id comes from the DATABASE now —
   * a client inventing its own is how two clients ended up handing the same
   * id to two different chats, one of which then adopted the other's running
   * CLI process.
   */
  async function create(workspaceId: number, opts?: { agentKind?: string }): Promise<ClaudeSession> {
    const agentKind = opts?.agentKind ?? 'claude';
    const transport: ChatTransport =
      (() => { const a = useProvidersStore().byId(agentKind); return a ? chatTransportFor(a) : (agentKind === 'claude' ? 'claude-cli' : 'acp'); })();
    const ws = useWorkspaceStore().workspaces.find((w) => w.id === workspaceId);
    const branch = ws?.worktree_branch || useGitStore().branchByWs[workspaceId] || undefined;
    const row = await invoke<ChatRow>("create_chat", {
      chat: rowFromSession({
        id: 0, // ignored — CreateChat assigns it
        workspaceId,
        claudeSessionId: "",
        title: `Chat ${sessionsForWs(workspaceId).length + 1}`,
        busy: false,
        messageCount: 0,
        agentKind,
        transport,
        lastActivityAt: Date.now(),
        branch,
      }),
    });

    const session = sessionFromRow(row);
    sessions.value.push(session);
    // Pass the REACTIVE array element (not the raw `session`) so the actor's
    // status mutations go through Vue's proxy and actually trigger reactivity.
    spawnActor(sessions.value[sessions.value.length - 1]);
    activeByWs.value[workspaceId] = session.id;
    // Only the per-device selection needs writing — the row is already in the
    // database, and a save_chats here would be a redundant round trip.
    setConfig(ACTIVE_KEY, activeByWs.value);
    return sessions.value[sessions.value.length - 1];
  }

  // Ensure at least one session exists for this workspace; return active.
  async function ensureSession(workspaceId: number): Promise<ClaudeSession> {
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
    // The chat is gone, so its stream session must go with it — otherwise its
    // listeners outlive it (they are deliberately kept across an unmount).
    dropChatSession(id);
    await invoke(s.transport === "claude-cli" ? "claude_stop" : s.transport === "codex-app-server" ? "codex_stop" : "acp_stop", { id }).catch(() => {});
    // Explicit delete: save_chats never removes, so a row only goes when
    // somebody says so — which is what stops a stale client from deleting a
    // chat it simply had not heard about yet.
    await invoke("delete_chat", { id }).catch(() => {});
    sessions.value = sessions.value.filter((x) => x.id !== id);
    // Hard delete — drop this chat's per-chat model / effort / permission mode /
    // ACP selections too, so config.json doesn't accumulate dead ids. (archive()
    // deliberately does NOT: an archived chat can be unarchived with its picks.)
    forgetChatSettings(id);
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

  // Mirror the chat's live model id for the Sidebar badge. Deliberately NOT
  // sync() — a model mirror is not activity and must not bump lastActivityAt.
  function setModel(id: number, model: string) {
    const s = sessions.value.find((x) => x.id === id);
    if (!s || s.model === model) return;
    s.model = model;
    persist();
  }

  function sendStatusEvent(id: number, event: AgentStatusEvent) {
    actors.get(id)?.send(event);
  }

  // Called by AgentChat on mount — which now only happens when the chat is
  // actually displayed (Terminal.isChatVisible), so "seen" means seen.
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
    setModel,
    permissionRules,
    addPermissionRule,
    hasPermissionRule,
    clearPermissionRules,
    sendStatusEvent,
    markSeen,
  };
});
