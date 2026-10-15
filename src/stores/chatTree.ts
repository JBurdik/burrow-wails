import type { ClaudeSession } from "./claudeChats";
import type { Phase } from "@/runtime/displayStatus";

/** How long a finished, seen, collected sub-agent stays in the Right Panel. */
export const SUBAGENT_AUTOARCHIVE_MS = 30 * 60 * 1000;

/** True when chatAttention's sweep may archive this sub-agent.
 *
 *  All of: a sub-agent, not archived, not busy and not on screen; its phase
 *  is settled (done/failed/idle — so no running turn and no open request,
 *  which Go turns into waiting_*); its result was taken by the parent
 *  (collected_at, set by wait_result/collect_results); this device has SEEN
 *  the last turn; and `delayMs` has passed since the later of that receipt
 *  and the last phase change. No phase = unknown = keep it. */
export function subagentArchivable(
  session: ClaudeSession,
  phase: Phase | undefined,
  seenAt: number,
  watching: boolean,
  now: number,
  delayMs = SUBAGENT_AUTOARCHIVE_MS,
): boolean {
  if (!session.parentChatId || session.archivedAt || session.busy || watching) return false;
  if (!session.collectedAt) return false;
  if (!phase || !["done", "failed", "idle"].includes(phase.state)) return false;
  if (!seenAt || seenAt < phase.turn_ended_at) return false;
  return now - Math.max(seenAt, phase.updated_at) >= delayMs;
}

/** Sub-agents of one thread, oldest first, for DISPLAY (the Right Panel's
 *  Sub-agents list). Pure so the panel filter
 *  is testable without a store, a transport or a mounted component.
 *
 *  Excludes archived children: archive(parent) stops each child's actor and
 *  its CLI process (claudeChats.ts), so a still-listed archived child is a
 *  dead entry that reopens (and restarts) a process the user just archived
 *  away — there is no "Archived" shelf for sub-agents the way there is for
 *  threads, so an archived child simply drops off this list.
 *
 *  Do NOT use this for teardown (remove()/archive()'s own recursion in
 *  claudeChats.ts) — an already-archived child still needs its row deleted
 *  and its listeners dropped when its PARENT is removed/archived, and this
 *  function is exactly what would skip it. Use allChildrenOf for that. */
export function childrenOf(sessions: ClaudeSession[], parentChatId: number): ClaudeSession[] {
  return sessions.filter((s) => s.parentChatId === parentChatId && !s.archivedAt).sort((a, b) => a.id - b.id);
}

/** Sub-agents of one thread, oldest first, INCLUDING archived ones — for
 *  TEARDOWN (remove()/archive()'s recursion in claudeChats.ts), never for
 *  display. A parent's remove()/archive() must reach every child regardless
 *  of archivedAt: Go cascades the DB rows on delete either way, and a child
 *  this recursion skips is a frontend session/actor/chatSession left
 *  dangling — alive in `sessions.value` with no row backing it — until the
 *  next reload. */
export function allChildrenOf(sessions: ClaudeSession[], parentChatId: number): ClaudeSession[] {
  return sessions.filter((s) => s.parentChatId === parentChatId).sort((a, b) => a.id - b.id);
}

/** Threads of one workspace: a sub-agent lives in the Right Panel, never in the
 *  Sidebar, so it is filtered out here rather than at each call site. */
export function topLevel(sessions: ClaudeSession[], workspaceId: number): ClaudeSession[] {
  return sessions.filter((s) => s.workspaceId === workspaceId && !s.parentChatId);
}

/** The chat a workspace is CURRENTLY showing, read off the tabs mirror.
 *
 *  Not `claudeChats.activeByWs`: nothing writes that slot any more, so
 *  a chat opened from the Sidebar or a route never reaches it and anything
 *  asking it "which thread is on screen" gets null while a thread is plainly
 *  open. The active TAB is what the user is looking at, and the mirror carries
 *  `chatId` for chat tabs. Returns null for a terminal tab, which is correct —
 *  no thread is on screen. */
export function activeChatIdFor(
  tabsByWs: Record<number, Array<{ id: number; chatId?: number }>>,
  activeByWs: Record<number, number>,
  workspaceId: number | undefined,
): number | null {
  if (!workspaceId) return null;
  const activeTab = activeByWs[workspaceId];
  if (activeTab == null) return null;
  return tabsByWs[workspaceId]?.find((t) => t.id === activeTab)?.chatId ?? null;
}
