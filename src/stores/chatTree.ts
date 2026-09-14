import type { ClaudeSession } from "./claudeChats";

/** Sub-agents of one thread, oldest first. Pure so the Sidebar/panel filter is
 *  testable without a store, a transport or a mounted component.
 *
 *  Excludes archived children: archive(parent) stops each child's actor and
 *  its CLI process (claudeChats.ts), so a still-listed archived child is a
 *  dead entry that reopens (and restarts) a process the user just archived
 *  away — there is no "Archived" shelf for sub-agents the way there is for
 *  threads, so an archived child simply drops off this list. */
export function childrenOf(sessions: ClaudeSession[], parentChatId: number): ClaudeSession[] {
  return sessions.filter((s) => s.parentChatId === parentChatId && !s.archivedAt).sort((a, b) => a.id - b.id);
}

/** Threads of one workspace: a sub-agent lives in the Right Panel, never in the
 *  Sidebar, so it is filtered out here rather than at each call site. */
export function topLevel(sessions: ClaudeSession[], workspaceId: number): ClaudeSession[] {
  return sessions.filter((s) => s.workspaceId === workspaceId && !s.parentChatId);
}
