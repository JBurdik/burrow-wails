import { reactive, watch } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { displayStatus, type Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import type { TermStatus } from "@/lib/terminalStatus";
import { useClaudeChatsStore } from "@/stores/claudeChats";

const SEEN_AT_KEY = "burrow.subagentSeenAt";

function loadSeenAt(): Map<number, number> {
  const receipts = new Map<number, number>();
  try {
    const raw = JSON.parse(localStorage.getItem(SEEN_AT_KEY) ?? "{}") as Record<string, number>;
    for (const [key, value] of Object.entries(raw)) {
      const id = Number(key);
      if (Number.isFinite(id) && typeof value === "number") receipts.set(id, value);
    }
  } catch {
    // A corrupt receipt cache only makes completed children appear unread.
  }
  return receipts;
}

function persistSeenAt(receipts: Map<number, number>): void {
  try {
    localStorage.setItem(SEEN_AT_KEY, JSON.stringify(Object.fromEntries(receipts)));
  } catch {
    // Read receipts are best effort; backend phase remains authoritative.
  }
}

/**
 * Shared phase/read-receipt model for chat-tree children.
 *
 * RightPanel used to derive a watched `done` status directly from the backend
 * with `seenAt = 0`, while Sidebar read an unrelated in-memory XState actor.
 * That made a completed child stay blue forever in one surface and disappear
 * after an app restart in the other. This store gives both surfaces one
 * backend-backed answer.
 */
export const useSubagentAttentionStore = defineStore("subagentAttention", () => {
  const chats = useClaudeChatsStore();
  const phases = reactive(new Map<number, Phase>());
  const seenAt = reactive(loadSeenAt());
  const watching = reactive(new Set<number>());
  const unlisteners = new Map<number, UnlistenFn>();
  const pendingListeners = new Set<number>();
  const phaseFromEvent = new Set<number>();

  function statusFor(chatId: number): TermStatus {
    return displayStatus(phases.get(chatId), seenAt.get(chatId) ?? 0, watching.has(chatId));
  }

  function syncSessionStatus(chatId: number): void {
    const session = chats.sessions.find((candidate) => candidate.id === chatId);
    if (session) session.status = statusFor(chatId);
  }

  function markSeen(chatId: number): void {
    const phase = phases.get(chatId);
    if (phase && phase.turn_ended_at > (seenAt.get(chatId) ?? 0)) {
      seenAt.set(chatId, Math.max(phase.turn_ended_at, Date.now()));
      persistSeenAt(seenAt);
    }
    chats.markSeen(chatId);
    syncSessionStatus(chatId);
  }

  function applyPhase(chatId: number, phase: Phase): void {
    phases.set(chatId, phase);
    // If the detail is already on screen, its result is read immediately.
    if (watching.has(chatId) && (phase.state === "done" || phase.state === "failed")) {
      markSeen(chatId);
      return;
    }
    syncSessionStatus(chatId);
  }

  function setWatching(chatId: number, value: boolean): void {
    if (value) {
      watching.add(chatId);
      markSeen(chatId);
      return;
    }
    watching.delete(chatId);
    syncSessionStatus(chatId);
  }

  watch(
    () => chats.sessions
      .filter((session) => session.parentChatId && !session.archivedAt)
      .map((session) => session.id),
    (ids) => {
      const liveIds = new Set(ids);
      for (const [id, unlisten] of unlisteners) {
        if (liveIds.has(id)) continue;
        unlisten();
        unlisteners.delete(id);
        pendingListeners.delete(id);
        phaseFromEvent.delete(id);
        phases.delete(id);
        watching.delete(id);
      }

      const freshIds = ids.filter((id) => !unlisteners.has(id) && !pendingListeners.has(id));
      if (freshIds.length === 0) return;
      freshIds.forEach((id) => pendingListeners.add(id));

      void invoke<ShellSnapshotData>("shell_snapshot").then((snapshot) => {
        for (const id of freshIds) {
          const phase = snapshot.phases[`chat:${id}`];
          if (phase && !phaseFromEvent.has(id)) applyPhase(id, phase);
        }
      }).catch(() => {});

      for (const id of freshIds) {
        void listen<Phase>(`phase-chat:${id}`, (event) => {
          if (!event.payload) return;
          phaseFromEvent.add(id);
          applyPhase(id, event.payload);
        }).then((unlisten) => {
          pendingListeners.delete(id);
          if (!chats.sessions.some((session) => session.id === id && session.parentChatId && !session.archivedAt)) {
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

  return { statusFor, markSeen, setWatching };
});
