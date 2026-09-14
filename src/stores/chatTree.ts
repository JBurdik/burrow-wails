import type { ClaudeSession } from "./claudeChats";

/** Sub-agents of one thread, oldest first. Pure so the Sidebar/panel filter is
 *  testable without a store, a transport or a mounted component. */
export function childrenOf(sessions: ClaudeSession[], parentChatId: number): ClaudeSession[] {
  return sessions.filter((s) => s.parentChatId === parentChatId).sort((a, b) => a.id - b.id);
}

/** Threads of one workspace: a sub-agent lives in the Right Panel, never in the
 *  Sidebar, so it is filtered out here rather than at each call site. */
export function topLevel(sessions: ClaudeSession[], workspaceId: number): ClaudeSession[] {
  return sessions.filter((s) => s.workspaceId === workspaceId && !s.parentChatId);
}
