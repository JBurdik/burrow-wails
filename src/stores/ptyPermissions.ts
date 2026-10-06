import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

/** One Claude PermissionRequest parked in Go (src-wails/ptypermissions.go). */
export interface PtyPermission {
  id: string;
  pty_id: string;
  tool_name: string;
  tool_input: unknown;
  at: number;
}

export type PermAnswer = "allow" | "deny" | "always";

/** One-line description of what the tool wants to do, for a card. */
export function describePermission(p: Pick<PtyPermission, "tool_name" | "tool_input">): string {
  const input = (p.tool_input ?? {}) as Record<string, unknown>;
  const detail = input.command ?? input.file_path ?? input.url ?? input.pattern ?? input.path;
  return typeof detail === "string" ? detail : JSON.stringify(input);
}

/** Oldest request first, plus how many more are queued behind it. */
export function headOfQueue(list: PtyPermission[]): { head: PtyPermission; more: number } | null {
  if (!list.length) return null;
  const sorted = [...list].sort((a, b) => a.at - b.at);
  return { head: sorted[0], more: sorted.length - 1 };
}

/**
 * Pending PTY permission requests, mirrored from Go: the `pty-permissions`
 * event carries a pty's WHOLE pending list (state, not a delta), so a missed
 * or replayed event cannot leave this wrong. Go owns closing a request
 * (answered, hook gone, phase left waiting_approval); this store only shows
 * and relays the answer.
 */
export const usePtyPermissionsStore = defineStore("ptyPermissions", () => {
  const byPty = ref<Record<string, PtyPermission[]>>({});
  // Toasts the user dismissed; the banner stays until the request closes.
  const toastDismissed = ref<Set<string>>(new Set());

  const all = computed(() =>
    Object.values(byPty.value).flat().sort((a, b) => a.at - b.at),
  );
  const toasts = computed(() => all.value.filter((p) => !toastDismissed.value.has(p.id)));

  function set(ptyId: string, list: PtyPermission[]) {
    if (list.length) byPty.value = { ...byPty.value, [ptyId]: list };
    else {
      const { [ptyId]: _gone, ...rest } = byPty.value;
      byPty.value = rest;
    }
  }

  async function refresh() {
    try {
      const list = (await invoke<PtyPermission[]>("list_pty_permissions")) ?? [];
      const next: Record<string, PtyPermission[]> = {};
      for (const p of list) (next[p.pty_id] ??= []).push(p);
      byPty.value = next;
    } catch {
      // Not connected yet; the next event or resync repopulates.
    }
  }

  async function answer(p: PtyPermission, behavior: PermAnswer, message = "") {
    try {
      await invoke("answer_pty_permission", { id: p.id, behavior, message });
    } catch {
      // Already closed (answered in the TUI, hook gone): the next event clears it.
    }
  }

  let started = false;
  function start() {
    if (started) return;
    started = true;
    void listen<{ pty_id: string; requests: PtyPermission[] }>("pty-permissions", (e) =>
      set(e.payload.pty_id, e.payload.requests ?? []),
    );
    void refresh();
  }

  return { byPty, toasts, all, start, refresh, answer, dismissToast: (id: string) => toastDismissed.value.add(id) };
});
