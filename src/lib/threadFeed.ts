import { computed, effectScope, onScopeDispose, ref, watch, type EffectScope } from "vue";
import { listen } from "@tauri-apps/api/event";
import { invoke } from "@tauri-apps/api/core";
import { appTransport, cachedRead } from "@/lib/wailsCompat/core";
import { displayStatus, type Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import type { TermStatus } from "@/lib/terminalStatus";
import { settledFor, useClaudeChatsStore, type ClaudeSession } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { onSeenChanged } from "@/lib/seenSync";

// The thread feed: every active thread of every project, grouped by what it
// needs from you. One model for the tray panel (TrayPanel.vue) and the phone's
// home (ThreadsScreen.vue). It reads Go's phases itself — one shell_snapshot,
// then the phase events of exactly the rows listed — rather than the mounted
// Terminals, so a project that isn't mounted (a phone mounts one) still shows.

export interface FeedRow {
  key: string;
  kind: "chat" | "tab";
  id: number;
  workspaceId: number;
  /** The repo a worktree belongs to (itself for a repo) — what a project filter matches. */
  repoId: number;
  title: string;
  workspace: string;
  detail: string;
  status: TermStatus;
  route: string;
  at: number;
  recap?: string;
  /** The project's logo (a worktree shows its repo's). */
  icon?: string;
}

export interface PtyPermRequest {
  id: string;
  pty_id: string;
  tool_name: string;
  tool_input: Record<string, unknown> | null;
}

export interface FeedGroup {
  key: "needs" | "working" | "unread" | "recent";
  label: string;
  rows: FeedRow[];
}

export const needsYou = (st: TermStatus) => st === "permission" || st === "waiting" || st === "error";
export const unread = (st: TermStatus) => st === "review" || st === "done";

/** Needs you → Working → Unread → Recent (idle, at most `recentCap`). Rows keep their order. */
export function feedGroups(rows: FeedRow[], recentCap = Infinity): FeedGroup[] {
  return [
    { key: "needs", label: "Needs you", rows: rows.filter((r) => needsYou(r.status)) },
    { key: "working", label: "Working", rows: rows.filter((r) => r.status === "running") },
    { key: "unread", label: "Unread", rows: rows.filter((r) => unread(r.status)) },
    { key: "recent", label: "Recent", rows: rows.filter((r) => r.status === "idle").slice(0, recentCap) },
  ];
}

/** Pure: the snapshot + chat rows → feed rows, newest first. `parentOf` maps a worktree to its repo. */
export function feedRows(
  snap: ShellSnapshotData,
  sessions: readonly ClaudeSession[],
  seen: Record<string, number>,
  now: number,
  meta: { icons?: Record<number, string | undefined>; parentOf?: Record<number, number | null | undefined> } = {},
): FeedRow[] {
  const byId = new Map(snap.workspaces.map((w) => [w.id, w]));
  const repoOf = (id: number) => meta.parentOf?.[id] ?? id;
  const icons = meta.icons ?? {};
  const out: FeedRow[] = [];
  for (const c of sessions) {
    if (c.archivedAt || c.parentChatId || c.kind === "side") continue;
    const phase = snap.phases[`chat:${c.id}`] as Phase | undefined;
    const status = displayStatus(phase, seen[`chat:${c.id}`] ?? 0, false);
    if (settledFor({ ...c, status }, now)) continue;
    out.push({
      key: `chat:${c.id}`, kind: "chat", id: c.id, workspaceId: c.workspaceId, repoId: repoOf(c.workspaceId),
      title: c.title || `Chat ${c.id}`, workspace: byId.get(c.workspaceId)?.name ?? "",
      detail: status === "permission" || status === "waiting" ? (phase?.detail ?? "") : "",
      status, route: `/ws/${c.workspaceId}?chat=${c.id}`, recap: c.recap, icon: icons[repoOf(c.workspaceId)],
      at: Math.max(c.lastActivityAt ?? 0, phase?.updated_at ?? 0),
    });
  }
  for (const [ws, tabs] of Object.entries(snap.tabs)) {
    const wsId = Number(ws);
    for (const t of tabs as { pty_id?: number | null; title?: string | null; default_title?: string | null }[]) {
      if (!t.pty_id) continue;
      const phase = snap.phases[`pty:${t.pty_id}`] as Phase | undefined;
      if (!phase?.is_agent) continue;
      out.push({
        key: `pty:${t.pty_id}`, kind: "tab", id: t.pty_id, workspaceId: wsId, repoId: repoOf(wsId),
        title: t.title || t.default_title || "Terminal", workspace: byId.get(wsId)?.name ?? "",
        detail: "", status: displayStatus(phase, seen[String(t.pty_id)] ?? 0, false),
        route: `/ws/${ws}/tab/${t.pty_id}`, at: phase.updated_at ?? 0, icon: icons[repoOf(wsId)],
      });
    }
  }
  return out.sort((a, b) => b.at - a.at);
}

export function permTarget(req: PtyPermRequest): string {
  const i = req.tool_input ?? {};
  return String(i.command ?? i.file_path ?? i.path ?? i.url ?? i.pattern ?? "").slice(0, 200);
}

function seenAt(): Record<string, number> {
  try {
    return JSON.parse(localStorage.getItem("burrow.seenAt") ?? "{}") as Record<string, number>;
  } catch {
    return {};
  }
}

// The feed is torn down when its last reader unmounts (the phone's Threads
// screen leaves the DOM while a chat is open). Keeping the last picture here
// lets coming back paint at once instead of empty until shell_snapshot answers.
let lastSnap: ShellSnapshotData | null = null;
let lastPerms: PtyPermRequest[] = [];

function createFeed() {
  const chats = useClaudeChatsStore();
  const workspaces = useWorkspaceStore();
  // First paint from the last snapshot this paired device saw (never noteSeq'd:
  // it is only a picture until load() replaces it).
  const snap = ref<ShellSnapshotData | null>(lastSnap ?? cachedRead<ShellSnapshotData>("shell_snapshot"));
  const perms = ref<PtyPermRequest[]>(lastPerms);
  watch(snap, (v) => { if (v) lastSnap = v; });
  watch(perms, (v) => { lastPerms = v; });
  const now = ref(Date.now());

  // One shell_snapshot for the first paint, then events: the phases of exactly
  // the chats and agent tabs listed, pty-permissions, terminal-tabs-changed
  // (one workspace's tabs), workspaces-changed. A re-read only on resync,
  // focus / visibility and a rare safety tick (it used to be every 15 s).
  async function load() {
    if (document.visibilityState === "hidden") return;
    now.value = Date.now();
    const [s, p] = await Promise.all([
      invoke<ShellSnapshotData>("shell_snapshot").catch(() => snap.value),
      invoke<PtyPermRequest[]>("list_pty_permissions").catch(() => perms.value),
    ]);
    snap.value = s;
    perms.value = p ?? [];
  }

  const rows = computed<FeedRow[]>(() => {
    if (!snap.value) return [];
    const parentOf: Record<number, number | null | undefined> = {};
    for (const w of workspaces.workspaces) parentOf[w.id] = w.parent_id;
    return feedRows(snap.value, chats.sessions, seenAt(), now.value, { icons: workspaces.icons, parentOf });
  });

  const phaseKeys = computed(() => {
    const keys = chats.sessions.filter((c) => !c.archivedAt && !c.parentChatId && c.kind !== "side").map((c) => `chat:${c.id}`);
    for (const tabs of Object.values(snap.value?.tabs ?? {})) {
      for (const t of tabs as { pty_id?: number | null }[]) if (t.pty_id) keys.push(`pty:${t.pty_id}`);
    }
    return keys;
  });
  const phaseListeners = new Map<string, Promise<() => void>>();
  watch(phaseKeys, (keys) => {
    const want = new Set(keys);
    for (const [key, off] of phaseListeners) {
      if (!want.has(key)) { void off.then((f) => f()); phaseListeners.delete(key); }
    }
    for (const key of want) {
      if (phaseListeners.has(key)) continue;
      phaseListeners.set(key, listen<Phase>(`phase-${key}`, (e) => {
        if (!snap.value || !e.payload) return;
        now.value = Date.now();
        snap.value = { ...snap.value, phases: { ...snap.value.phases, [key]: e.payload } };
      }));
    }
  }, { immediate: true });

  async function answerPerm(req: PtyPermRequest, behavior: "allow" | "always" | "deny") {
    perms.value = perms.value.filter((p) => p.id !== req.id);
    await invoke("answer_pty_permission", { id: req.id, behavior, message: "" }).catch(() => {});
    void load();
  }
  const permsFor = (row: FeedRow) => (row.kind === "tab" ? perms.value.filter((p) => p.pty_id === String(row.id)) : []);

  // A seen receipt (burrow.seenAt) changes a row's status with no event: the
  // other window's write arrives as `storage`, this window's own as a re-read.
  const tick = () => { now.value = Date.now(); };
  const permsOff = listen("pty-permissions", () => {
    void invoke<PtyPermRequest[]>("list_pty_permissions").then((p) => { perms.value = p ?? []; }).catch(() => {});
  });
  const tabsOff = listen<{ workspaceId: number }>("terminal-tabs-changed", (e) => {
    const wsId = e.payload?.workspaceId;
    if (!wsId) return;
    void invoke<ShellSnapshotData["tabs"][number]>("list_terminal_tabs", { workspaceId: wsId }).then((tabs) => {
      if (snap.value) snap.value = { ...snap.value, tabs: { ...snap.value.tabs, [wsId]: tabs ?? [] } };
    }).catch(() => {});
  });
  const wsOff = listen("workspaces-changed", () => void load());
  let offResync = () => {};
  try {
    offResync = appTransport().onResync(() => void load());
  } catch {
    // no backend (tests, browser-only dev)
  }
  // ponytail: safety net for anything without an event; also moves `now` for
  // the 3-day settle flip. Drop when nothing is found missing.
  const resync = window.setInterval(load, 5 * 60_000);
  window.addEventListener("focus", load);
  window.addEventListener("storage", tick);
  const offSeen = onSeenChanged(tick);
  document.addEventListener("visibilitychange", load);
  void load();
  onScopeDispose(() => {
    clearInterval(resync);
    window.removeEventListener("focus", load);
    window.removeEventListener("storage", tick);
    offSeen();
    document.removeEventListener("visibilitychange", load);
    void permsOff.then((f) => f());
    void tabsOff.then((f) => f());
    void wsOff.then((f) => f());
    offResync();
    for (const off of phaseListeners.values()) void off.then((f) => f());
  });

  return { rows, perms, now, load, answerPerm, permsFor };
}

export type ThreadFeed = ReturnType<typeof createFeed>;

// One feed per window, however many components read it (the phone's home and
// its bottom-nav badge both do) — each would otherwise hold its own listeners.
let scope: EffectScope | null = null;
let shared: ThreadFeed | null = null;
let users = 0;

export function useThreadFeed(): ThreadFeed {
  if (!shared) {
    scope = effectScope(true);
    shared = scope.run(createFeed)!;
  }
  users++;
  onScopeDispose(() => {
    if (--users > 0) return;
    scope?.stop();
    scope = null;
    shared = null;
  });
  return shared;
}
