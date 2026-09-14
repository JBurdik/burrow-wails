import type { ClaudeSession } from "./claudeChats";

/** Sub-agents of one thread, oldest first, for DISPLAY (SubAgentHost.vue's
 *  mount list, the Right Panel's Sub-agents list). Pure so the panel filter
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
