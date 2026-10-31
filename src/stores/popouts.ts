import { defineStore } from "pinia";
import { reactive } from "vue";
import { listen } from "@tauri-apps/api/event";
import { closeWindow, openWindow } from "@/lib/windows";

/** A terminal or chat leaf shown in its own window (Window → "Open in window"). */
export interface Popout {
  leafId: number;
  wsId: number;
  kind: "terminal" | "chat";
  chatId?: number;
  cwd: string;
  title: string;
}

const windowKey = (leafId: number) => `tab-${leafId}`;

// The main window keeps owning the leaf: it stays in the Sidebar and its
// XTerm stays mounted but frozen (no resize_pty), so bringing it back never
// re-attaches the PTY. Closing the popped window — or "Bring back", which
// closes it — ends the popout via Go's window-closed event.
export const usePopoutsStore = defineStore("popouts", () => {
  const popped = reactive(new Map<number, Popout>());
  let listening: Promise<unknown> | null = null;

  function ensureListener() {
    listening ??= listen<string>("window-closed", ({ payload }) => {
      const m = /^tab-(\d+)$/.exec(String(payload));
      if (m) popped.delete(Number(m[1]));
    });
  }

  function popOut(p: Popout) {
    ensureListener();
    popped.set(p.leafId, p);
    const q = new URLSearchParams({ ws: String(p.wsId), cwd: p.cwd, title: p.title });
    if (p.chatId != null) q.set("chat", String(p.chatId));
    void openWindow(windowKey(p.leafId), `/window/${p.kind}/${p.leafId}?${q}`, p.title, 960, 680).catch(() => {
      popped.delete(p.leafId);
    });
  }

  function bringBack(leafId: number) {
    void closeWindow(windowKey(leafId));
  }

  const isPopped = (leafId: number) => popped.has(leafId);

  return { popped, popOut, bringBack, isPopped };
});
