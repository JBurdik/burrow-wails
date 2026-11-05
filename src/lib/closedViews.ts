// ⌘⇧T "reopen closed view", like a browser. In-memory LIFO of what the user
// closed: a terminal leaf (relaunched as a new tab with the same command + cwd —
// scrollback is gone with the PTY) or a chat leaf (unarchived and focused).
// ponytail: module-level array, not persisted — a restart forgets it, as a
// browser's does for a fresh window.

export type ClosedView =
  | { kind: "terminal"; wsId: number; cmd?: string; cwd?: string }
  | { kind: "chat"; wsId: number; chatId: number };

const MAX = 20;
const stack: ClosedView[] = [];

export function pushClosedView(v: ClosedView): void {
  // A chat closed twice (reopened, closed again) is one view: keep the newest.
  if (v.kind === "chat") {
    const i = stack.findIndex((x) => x.kind === "chat" && x.chatId === v.chatId);
    if (i >= 0) stack.splice(i, 1);
  }
  stack.push(v);
  if (stack.length > MAX) stack.shift();
}

export function popClosedView(): ClosedView | undefined {
  return stack.pop();
}

/** Test seam. */
export function clearClosedViews(): void {
  stack.length = 0;
}
