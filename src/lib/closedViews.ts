// ⌘⇧T "reopen closed view", like a browser. In-memory LIFO of what the user
// closed: a terminal leaf (relaunched as a new tab with the same command + cwd —
// scrollback is gone with the PTY) or a chat leaf (unarchived and focused).
// ponytail: module-level array, not persisted — a restart forgets it, as a
// browser's does for a fresh window.

export type ClosedView =
  | { kind: "terminal"; wsId: number; cmd?: string; cwd?: string; at?: number }
  | { kind: "chat"; wsId: number; chatId: number; at?: number };

const MAX = 20;
const stack: ClosedView[] = [];

export function pushClosedView(v: ClosedView): void {
  // A chat closed twice (reopened, closed again) is one view: keep the newest.
  if (v.kind === "chat") {
    const i = stack.findIndex((x) => x.kind === "chat" && x.chatId === v.chatId);
    if (i >= 0) stack.splice(i, 1);
  }
  stack.push({ ...v, at: v.at ?? Date.now() });
  if (stack.length > MAX) stack.shift();
}

export function popClosedView(): ClosedView | undefined {
  return stack.pop();
}

export function peekClosedView(): ClosedView | undefined {
  return stack[stack.length - 1];
}

/** What ⌘⇧T should reopen: the newest of the in-memory stack and the most
 *  recently archived thread (archived from anywhere — the row menu, a delete
 *  undo window, a previous run — the stack only sees tab/pane closes). */
export function pickReopen(
  top: ClosedView | undefined,
  archived: { id: number; workspaceId: number; archivedAt: number } | undefined,
): { from: "stack" } | { from: "archive"; view: ClosedView } | null {
  if (archived && (!top || (top.at ?? 0) < archived.archivedAt)) {
    return { from: "archive", view: { kind: "chat", wsId: archived.workspaceId, chatId: archived.id, at: archived.archivedAt } };
  }
  return top ? { from: "stack" } : null;
}

/** Test seam. */
export function clearClosedViews(): void {
  stack.length = 0;
}
