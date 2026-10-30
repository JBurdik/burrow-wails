import { reactive, watch } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { DONE_AUTOCLEAR_MS, displayStatus, shouldMarkSeen, type Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import type { TermStatus } from "@/lib/terminalStatus";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { subagentArchivable } from "@/stores/chatTree";
import { liveChatSessionIds } from "@/lib/chatSession";
import { appChatHost, notifyWoke } from "@/lib/chatHost";

// Shared with Terminal.vue's PTY receipts: one key, one receipt per subject.
// PTY leaves use their bare numeric id, chats `chat:<id>` — Terminal.vue's
// loader skips non-numeric keys and every writer is read-modify-write.
const SEEN_AT_KEY = "burrow.seenAt";
const LEGACY_SUBAGENT_KEY = "burrow.subagentSeenAt";
const chatKey = (id: number) => `chat:${id}`;

function loadSeenAt(): Map<number, number> {
  const receipts = new Map<number, number>();
  try {
    const raw = JSON.parse(localStorage.getItem(SEEN_AT_KEY) ?? "{}") as Record<string, number>;
    for (const [key, value] of Object.entries(raw)) {
      const id = Number(key.startsWith("chat:") ? key.slice(5) : NaN);
      if (Number.isFinite(id) && typeof value === "number") receipts.set(id, value);
    }
    // One-time carry-over from when only sub-agents had receipts.
    const legacy = JSON.parse(localStorage.getItem(LEGACY_SUBAGENT_KEY) ?? "null") as Record<string, number> | null;
    if (legacy) {
      for (const [key, value] of Object.entries(legacy)) {
        const id = Number(key);
        if (Number.isFinite(id) && typeof value === "number" && !receipts.has(id)) receipts.set(id, value);
      }
      persistSeenAt(receipts);
      localStorage.removeItem(LEGACY_SUBAGENT_KEY);
    }
  } catch {
    // A corrupt receipt cache only makes finished chats appear unread.
  }
  return receipts;
}

function persistSeenAt(receipts: Map<number, number>, remove?: number): void {
  try {
    const stored = JSON.parse(localStorage.getItem(SEEN_AT_KEY) ?? "{}") as Record<string, number>;
    for (const [id, at] of receipts) stored[chatKey(id)] = at;
    if (remove !== undefined) delete stored[chatKey(remove)];
    localStorage.setItem(SEEN_AT_KEY, JSON.stringify(stored));
  } catch {
    // Read receipts are best effort; backend phase remains authoritative.
  }
}

function windowFocused(): boolean {
  return typeof document === "undefined" || document.hasFocus();
}

/**
 * The displayed status of every chat: `displayStatus(phase, seenAt, watching)`,
 * the same derivation PTY leaves use in Terminal.vue.
 *
 * The phase is Go's (`phase-chat:<id>`); this store only adds what the client
 * owns — whether it has looked. It is the one writer of `session.status`,
 * which Sidebar, tab dots, Dashboard and the sub-agent summaries read.
 *
 * "Watching" is a count, not a flag: the chat's own leaf and the Right Panel's
 * sub-agent detail can both have the same chat on screen.
 */
export const useChatAttentionStore = defineStore("chatAttention", () => {
  const chats = useClaudeChatsStore();
  const phases = reactive(new Map<number, Phase>());
  const seenAt = reactive(loadSeenAt());
  const watchers = reactive(new Map<number, number>());
  const doneTimers = new Map<number, ReturnType<typeof setTimeout>>();
  const unlisteners = new Map<number, UnlistenFn>();
  const pendingListeners = new Set<number>();
  const phaseFromEvent = new Set<number>();

  function isWatching(chatId: number): boolean {
    return (watchers.get(chatId) ?? 0) > 0 && windowFocused();
  }

  function statusFor(chatId: number): TermStatus {
    return displayStatus(phases.get(chatId), seenAt.get(chatId) ?? 0, isWatching(chatId));
  }

  function phaseFor(chatId: number): Phase | undefined {
    return phases.get(chatId);
  }

  function refresh(chatId: number): void {
    const status = statusFor(chatId);
    const session = chats.sessions.find((candidate) => candidate.id === chatId);
    if (session) session.status = status;
    // A finished turn the user is watching stays lime for a moment, then
    // marks itself read — Terminal.vue's transient `done`, same rule.
    clearTimeout(doneTimers.get(chatId));
    doneTimers.delete(chatId);
    if (status === "done" && shouldMarkSeen(phases.get(chatId), isWatching(chatId))) {
      doneTimers.set(chatId, setTimeout(() => markSeen(chatId), DONE_AUTOCLEAR_MS));
    }
  }

  /** This client has looked at the chat's latest turn. Idempotent. */
  function markSeen(chatId: number): void {
    const phase = phases.get(chatId);
    if (phase && phase.turn_ended_at > (seenAt.get(chatId) ?? 0)) {
      seenAt.set(chatId, Math.max(phase.turn_ended_at, Date.now()));
      persistSeenAt(seenAt);
    }
    refresh(chatId);
  }

  function applyPhase(chatId: number, phase: Phase, live = false): void {
    const before = phases.get(chatId)?.state;
    phases.set(chatId, phase);
    refresh(chatId);
    if (live && phase.state !== before) notifyBlocked(chatId, phase);
  }

  // A chat with a session in the registry raises its own permission toast,
  // with the request's detail (chatSession.ts). One with none — a sub-agent
  // the Go runtime started that nobody has opened, a thread whose view is
  // gone — would block silently, so the phase raises it here instead.
  function notifyBlocked(chatId: number, phase: Phase): void {
    if (phase.state !== "waiting_approval" && phase.state !== "waiting_input") return;
    if (liveChatSessionIds().includes(chatId)) return;
    appChatHost.notifyPermission(chatId, { requestId: "", toolName: phase.detail || "Agent", input: {}, suggestions: [] });
  }

  function setWatching(chatId: number, value: boolean): void {
    const count = Math.max(0, (watchers.get(chatId) ?? 0) + (value ? 1 : -1));
    if (count === 0) watchers.delete(chatId);
    else watchers.set(chatId, count);
    if (value && windowFocused()) {
      markSeen(chatId);
      markActive(chatId);
    } else refresh(chatId);
  }

  // Auto-sleep (src-wails/chatsleep.go) never stops a chat that is on screen
  // while the window has focus — Go learns that from this heartbeat, which
  // counts as activity. Well inside the shortest (15 min) sleep window.
  function markActive(chatId: number): void {
    void invoke("chat_mark_active", { chatId }).catch(() => {});
  }
  setInterval(() => {
    for (const chatId of watchers.keys()) if (isWatching(chatId)) markActive(chatId);
  }, 60_000);

  function forget(chatId: number): void {
    unlisteners.get(chatId)?.();
    unlisteners.delete(chatId);
    pendingListeners.delete(chatId);
    phaseFromEvent.delete(chatId);
    phases.delete(chatId);
    watchers.delete(chatId);
    clearTimeout(doneTimers.get(chatId));
    doneTimers.delete(chatId);
  }

  const isLive = (id: number) => chats.sessions.some((session) => session.id === id && !session.archivedAt);

  watch(
    () => chats.sessions.filter((session) => !session.archivedAt).map((session) => session.id),
    (ids) => {
      const liveIds = new Set(ids);
      for (const id of [...unlisteners.keys(), ...pendingListeners]) {
        if (!liveIds.has(id)) forget(id);
      }

      const freshIds = ids.filter((id) => !unlisteners.has(id) && !pendingListeners.has(id));
      if (freshIds.length === 0) return;
      freshIds.forEach((id) => pendingListeners.add(id));

      void invoke<ShellSnapshotData>("shell_snapshot").then((snapshot) => {
        for (const id of freshIds) {
          const phase = snapshot.phases[chatKey(id)];
          if (phase && !phaseFromEvent.has(id)) applyPhase(id, phase);
        }
      }).catch(() => {});

      for (const id of freshIds) {
        void listen<Phase>(`phase-chat:${id}`, (event) => {
          if (!event.payload) return;
          phaseFromEvent.add(id);
          applyPhase(id, event.payload, true);
        }).then((unlisten) => {
          pendingListeners.delete(id);
          if (!isLive(id)) {
            unlisten();
            return;
          }
          unlisteners.set(id, unlisten);
        }).catch(() => {
          pendingListeners.delete(id);
        });
      }
    },
    { immediate: true },
  );

  // A snooze ran out (src-wails/chatsnooze.go — Go already cleared the settle
  // pin and bumped last_activity_at, so the row floats up). Dropping this
  // device's receipt brings back the review dot on a finished turn.
  void listen<{ chat_id: number }>("chat-snooze-woke", (event) => {
    const chatId = event.payload?.chat_id;
    if (!chatId || !isLive(chatId)) return;
    seenAt.delete(chatId);
    persistSeenAt(seenAt, chatId);
    refresh(chatId);
    notifyWoke(chatId);
  });

  // Sub-agent auto-archive. Lives here, not in Go, because "seen" is this
  // device's receipt (localStorage) — Go has the phase and collected_at but
  // no idea whether anyone looked. Only the desktop instantiates this store,
  // so the phone never archives. Same archive() as the Sidebar's button: one
  // archived_at column, process stopped.
  // ponytail: 1-minute poll; a per-chat timer only matters if 30 min ± 1 does.
  setInterval(() => {
    const now = Date.now();
    for (const session of chats.sessions) {
      if (subagentArchivable(session, phases.get(session.id), seenAt.get(session.id) ?? 0, (watchers.get(session.id) ?? 0) > 0, now)) {
        void chats.archive(session.id);
      }
    }
  }, 60_000);

  return { statusFor, phaseFor, markSeen, setWatching };
});
