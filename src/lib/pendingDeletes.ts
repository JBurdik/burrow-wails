import { reactive } from "vue";

// Undo window for a destructive action (t3code's SidebarThreadUndoNotice): the
// rows are hidden at once, the real delete runs only when the window expires.
// Nothing is persisted — quitting inside the window keeps the chat, which is
// the safe direction. Each schedule() is its own timer, so a second delete
// neither flushes nor cancels the first.
export function createPendingDeletes(windowMs = 8000) {
  const hidden = reactive(new Set<number>());

  function schedule(ids: number[], commit: () => void | Promise<void>): () => void {
    for (const id of ids) hidden.add(id);
    let done = false;
    const release = () => { for (const id of ids) hidden.delete(id); };
    const timer = setTimeout(async () => {
      done = true;
      // Unhide only after the commit, or the rows flash back before they go.
      try { await commit(); } finally { release(); }
    }, windowMs);
    // Undo: a no-op once the commit has started.
    return () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      release();
    };
  }

  return { hidden, schedule, isHidden: (id: number | null | undefined) => id != null && hidden.has(id) };
}
