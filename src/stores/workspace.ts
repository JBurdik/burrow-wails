import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { invoke } from "@tauri-apps/api/core";

export interface Workspace {
  id: number;
  name: string;
  path: string;
  created_at: number;
  last_opened: number | null;
  parent_id?: number | null;
  worktree_branch?: string | null;
  // Whether the directory is a git repo. Non-git folders hide all git UI.
  // Older payloads may omit it — treat undefined as git (back-compat).
  is_git?: boolean;
  icon?: string | null;
  sort_order: number;
}

export const useWorkspaceStore = defineStore("workspace", () => {
  const workspaces = ref<Workspace[]>([]);
  const active = ref<Workspace | null>(null);
  // Workspaces opened this session — each keeps its Terminal (and PTYs) mounted
  // so switching between them never tears down running processes.
  const opened = ref<Workspace[]>([]);

  // Top-level repo workspaces (no parent), ordered by the SQLite-backed
  // sort_order column. Worktrees are nested under their parent.
  const topLevel = computed(() => {
    const tops = workspaces.value.filter((w) => !w.parent_id);
    return [...tops].sort((a, b) => a.sort_order - b.sort_order || a.id - b.id);
  });

  // Move a top-level workspace from one visible position to another, persisting
  // the new order. Indices are into `topLevel`.
  function reorderTopLevel(from: number, to: number) {
    const ids = topLevel.value.map((w) => w.id);
    if (from < 0 || from >= ids.length || to < 0 || to >= ids.length) return;
    const [moved] = ids.splice(from, 1);
    ids.splice(to, 0, moved);
    ids.forEach((id, i) => {
      const w = workspaces.value.find((x) => x.id === id);
      if (w) w.sort_order = i;
    });
    invoke("set_workspace_order", { ids }).catch(() => {});
  }
  // Worktree rows grouped by their parent repo id.
  const worktreesByParent = computed(() => {
    const m: Record<number, Workspace[]> = {};
    for (const w of workspaces.value) {
      if (w.parent_id) (m[w.parent_id] ??= []).push(w);
    }
    return m;
  });

  const icons = computed(() => {
    const m: Record<number, string> = {};
    for (const w of workspaces.value) if (w.icon) m[w.id] = w.icon;
    return m;
  });

  function setIcon(id: number, dataUrl: string) {
    const w = workspaces.value.find((x) => x.id === id);
    if (w) w.icon = dataUrl;
    invoke("set_workspace_icon", { id, icon: dataUrl }).catch(() => {});
  }

  function clearIcon(id: number) {
    const w = workspaces.value.find((x) => x.id === id);
    if (w) w.icon = null;
    invoke("set_workspace_icon", { id, icon: null }).catch(() => {});
  }

  async function migrateLegacyLocalStorage(): Promise<boolean> {
    let migrated = false;
    const iconsRaw = localStorage.getItem("ws-icons");
    if (iconsRaw) {
      migrated = true;
      try {
        const legacy: Record<string, string> = JSON.parse(iconsRaw);
        for (const [idStr, dataUrl] of Object.entries(legacy)) {
          const id = Number(idStr);
          if (workspaces.value.some((w) => w.id === id)) {
            await invoke("set_workspace_icon", { id, icon: dataUrl }).catch(() => {});
          }
        }
      } catch {}
      localStorage.removeItem("ws-icons");
    }
    const orderRaw = localStorage.getItem("burrow.ws.order");
    if (orderRaw) {
      migrated = true;
      try {
        const legacyOrder: number[] = JSON.parse(orderRaw);
        const known = legacyOrder.filter((id) => workspaces.value.some((w) => w.id === id));
        if (known.length) await invoke("set_workspace_order", { ids: known }).catch(() => {});
      } catch {}
      localStorage.removeItem("burrow.ws.order");
    }
    return migrated;
  }

  async function load() {
    workspaces.value = await invoke<Workspace[]>("list_workspaces");
    const migrated = await migrateLegacyLocalStorage();
    if (migrated) {
      workspaces.value = await invoke<Workspace[]>("list_workspaces"); // re-fetch with migrated values
    }
  }

  async function create(name: string, path: string): Promise<Workspace> {
    const ws = await invoke<Workspace>("create_workspace", { name, path });
    await load();
    return ws;
  }

  async function createWorktree(
    parentId: number,
    branch: string,
    baseRef: string | null,
    path: string,
  ): Promise<Workspace> {
    const ws = await invoke<Workspace>("create_worktree", {
      parentId,
      branch,
      baseRef: baseRef || null,
      path,
    });
    await load();
    return ws;
  }

  async function removeWorktree(id: number, force = false) {
    await invoke("remove_worktree", { id, force });
    workspaces.value = workspaces.value.filter((w) => w.id !== id);
    opened.value = opened.value.filter((w) => w.id !== id);
    if (active.value?.id === id) active.value = null;
    clearIcon(id);
  }

  async function remove(id: number) {
    // Remove any child worktrees first so we don't leave dangling git worktrees
    // or orphaned rows pointing at a deleted parent.
    const children = worktreesByParent.value[id] || [];
    for (const wt of children) {
      try {
        await removeWorktree(wt.id);
      } catch {
        // best-effort: keep going so the parent can still be deleted
      }
    }
    await invoke("delete_workspace", { id });
    workspaces.value = workspaces.value.filter((w) => w.id !== id);
    opened.value = opened.value.filter((w) => w.id !== id);
    if (active.value?.id === id) active.value = null;
    clearIcon(id);
  }

  async function rename(id: number, name: string) {
    await invoke("rename_workspace", { id, name });
    const w = workspaces.value.find((x) => x.id === id);
    if (w) w.name = name;
    if (active.value?.id === id) active.value.name = name;
    const o = opened.value.find((x) => x.id === id);
    if (o) o.name = name;
  }

  // Switch first, persist after: a failing/slow touch_workspace must never block
  // the UI from switching workspace.
  function open(ws: Workspace) {
    if (!opened.value.some((w) => w.id === ws.id)) opened.value.push(ws);
    active.value = ws;
    invoke("touch_workspace", { id: ws.id }).catch(() => {});
  }

  // Mount a workspace's Terminal (so it reattaches sessions / syncs tabs) WITHOUT
  // making it active. Used to eager-mount worktrees under an expanded parent.
  function ensureOpen(ws: Workspace) {
    if (!opened.value.some((w) => w.id === ws.id)) opened.value.push(ws);
  }

  // Back to the picker: keep `opened` (and its live terminals) intact.
  function close() { active.value = null; }

  return {
    workspaces, active, opened, icons, topLevel, worktreesByParent,
    load, create, remove, rename, open, ensureOpen, close, setIcon, clearIcon,
    createWorktree, removeWorktree, reorderTopLevel,
  };
});
