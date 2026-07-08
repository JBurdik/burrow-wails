<template>
  <div class="wsp" ref="rootEl">
    <button class="wsp-trigger" :class="{ open }" @click.stop="toggle">
      <div class="wsp-icon-wrap">
        <img v-if="active && store.icons[active.id]" :src="store.icons[active.id]" class="wsp-custom-icon" />
        <PhGitBranch v-else-if="active?.parent_id" :size="14" class="wsp-icon wt" />
        <PhFolder v-else :size="14" weight="fill" class="wsp-icon" />
      </div>
      <div class="wsp-info">
        <div class="wsp-name">{{ active?.name ?? "No workspace" }}</div>
        <div v-if="active" class="wsp-sub">{{ active.worktree_branch || shortPath(active.path) }}</div>
      </div>
      <span v-if="otherTabs" class="wsp-count other" :title="`${otherTabs} tabs in other workspaces`">+{{ otherTabs }}</span>
      <span v-if="otherStatus" class="status-dot" :class="`status-${otherStatus}`" title="Activity in other workspaces">{{ otherStatus === 'running' ? spinnerFrame : '' }}</span>
      <PhCaretDown :size="12" weight="bold" class="wsp-caret" />
    </button>

    <!-- Teleported: .sidebar is overflow:hidden, which clipped the menu and let
         clicks fall through to the tab list underneath. -->
    <Teleport to="body">
    <div v-if="open" class="wsp-menu" :style="menuStyle" @click.stop>
      <div v-if="store.topLevel.length === 0" class="wsp-empty">
        No workspaces.<br />Open a folder to start.
      </div>

      <template v-for="ws in store.topLevel" :key="ws.id">
        <div
          class="wsp-row"
          :class="{ active: active?.id === ws.id }"
          @click="select(ws)"
          @contextmenu.prevent.stop="openCtx(ws, $event, 'ws')"
        >
          <div class="wsp-icon-wrap sm">
            <img v-if="store.icons[ws.id]" :src="store.icons[ws.id]" class="wsp-custom-icon" />
            <PhFolder v-else :size="13" weight="fill" class="wsp-icon" />
          </div>
          <span class="wsp-row-name">{{ ws.name }}</span>
          <span v-if="tabCount(ws.id)" class="wsp-count" :title="`${tabCount(ws.id)} tabs`">{{ tabCount(ws.id) }}</span>
          <span
            v-if="git.prByWs[ws.id]"
            class="pr-badge"
            :class="`pr-${prTone(git.prByWs[ws.id]!)}`"
            :title="prTitle(git.prByWs[ws.id]!)"
          ><span class="pr-dot" />#{{ git.prByWs[ws.id]!.number }}</span>
          <span v-if="aggStatus(ws.id)" class="status-dot" :class="`status-${aggStatus(ws.id)}`">{{ aggStatus(ws.id) === 'running' ? spinnerFrame : '' }}</span>
          <button class="wsp-btn" title="New worktree" @click.stop="newWorktree(ws)"><PhGitBranch :size="12" /></button>
          <button
            class="wsp-btn pin"
            :class="{ on: isPinned(ws.id) }"
            :title="isPinned(ws.id) ? 'Unpin from sidebar' : `Pin to sidebar (max ${MAX_PINNED})`"
            @click.stop="togglePin(ws.id)"
          ><PhPushPin :size="12" :weight="isPinned(ws.id) ? 'fill' : 'regular'" /></button>
        </div>

        <div
          v-for="wt in store.worktreesByParent[ws.id] || []"
          :key="wt.id"
          class="wsp-row wsp-wt"
          :class="{ active: active?.id === wt.id }"
          :title="wt.path"
          @click="select(wt)"
          @contextmenu.prevent.stop="openCtx(wt, $event, 'wt')"
        >
          <PhGitBranch :size="12" class="wsp-icon wt" />
          <span class="wsp-row-name">{{ wt.worktree_branch || wt.name }}</span>
          <span v-if="tabCount(wt.id)" class="wsp-count" :title="`${tabCount(wt.id)} tabs`">{{ tabCount(wt.id) }}</span>
          <span
            v-if="git.prByWs[wt.id]"
            class="pr-badge"
            :class="`pr-${prTone(git.prByWs[wt.id]!)}`"
            :title="prTitle(git.prByWs[wt.id]!)"
          ><span class="pr-dot" />#{{ git.prByWs[wt.id]!.number }}</span>
          <span v-if="aggStatus(wt.id)" class="status-dot" :class="`status-${aggStatus(wt.id)}`">{{ aggStatus(wt.id) === 'running' ? spinnerFrame : '' }}</span>
          <button
            class="wsp-btn pin"
            :class="{ on: isPinned(wt.id) }"
            :title="isPinned(wt.id) ? 'Unpin from sidebar' : `Pin to sidebar (max ${MAX_PINNED})`"
            @click.stop="togglePin(wt.id)"
          ><PhPushPin :size="12" :weight="isPinned(wt.id) ? 'fill' : 'regular'" /></button>
        </div>
      </template>

      <div class="wsp-sep" />
      <button class="wsp-action" @click="open = false; emit('pick-folder')">
        <PhFolderPlus :size="13" />Open folder…
      </button>
    </div>
    </Teleport>

    <!-- Context menu -->
    <Teleport to="body">
      <div
        v-if="ctx"
        class="ctx-menu"
        :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }"
        @click.stop
        @contextmenu.prevent.stop
      >
        <template v-if="ctx.kind === 'ws'">
          <button class="ctx-item" @click="run(() => emit('rename', ctxWs()!))"><PhPencilSimple :size="13" />Rename…</button>
          <button class="ctx-item" @click="run(() => pickIcon(ctx!.id))"><PhImage :size="13" />Change icon…</button>
          <button v-if="store.icons[ctx.id]" class="ctx-item" @click="run(() => store.clearIcon(ctx!.id))"><PhImage :size="13" />Reset icon</button>
          <button class="ctx-item" @click="run(() => ui.openBoard(ctx!.id))"><PhKanban :size="13" />Board</button>
          <div class="ctx-sep" />
          <button class="ctx-item ctx-danger" @click="run(() => store.remove(ctx!.id))"><PhTrash :size="13" />Remove</button>
        </template>
        <template v-else>
          <button class="ctx-item ctx-danger" @click="run(removeWorktree)"><PhTrash :size="13" />Remove worktree</button>
        </template>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue";
import {
  PhFolder, PhFolderPlus, PhGitBranch, PhCaretDown, PhPushPin,
  PhPencilSimple, PhImage, PhTrash, PhKanban,
} from "@phosphor-icons/vue";
import { isPinned, togglePin, MAX_PINNED } from "@/lib/pinnedWorkspaces";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { useGitStore, type PrInfo } from "@/stores/git";
import { spinnerFrame } from "@/lib/spinner";
import { aggregateStatus, type TermStatus } from "@/lib/terminalStatus";

const emit = defineEmits<{
  (e: "pick-folder"): void;
  (e: "rename", ws: Workspace): void;
  (e: "new-worktree", ws: Workspace): void;
}>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();

const open = ref(false);
const rootEl = ref<HTMLElement>();
const active = computed(() => store.active);

// The menu is teleported to body, so it needs the trigger's viewport rect.
const menuStyle = ref<Record<string, string>>({});
function toggle() {
  if (!open.value && rootEl.value) {
    const r = rootEl.value.getBoundingClientRect();
    menuStyle.value = {
      left: `${r.left + 6}px`,
      top: `${r.bottom - 4}px`,
      width: `${r.width - 12}px`,
    };
  }
  open.value = !open.value;
}

function select(ws: Workspace) {
  open.value = false;
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(ws);
  // Keep the repo's worktrees mounted so their status dots stay live in the menu.
  const parent = ws.parent_id ?? ws.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
}

function newWorktree(ws: Workspace) {
  open.value = false;
  emit("new-worktree", ws);
}

function tabCount(id: number): number {
  return (termTabs.tabsByWs[id] || []).length;
}

// Tabs living in workspaces you can't currently see — "+7" on the trigger.
const otherTabs = computed(() =>
  store.workspaces
    .filter((w) => w.id !== active.value?.id)
    .reduce((n, w) => n + tabCount(w.id), 0),
);

function aggStatus(id: number): TermStatus | null {
  const s = aggregateStatus(termTabs.tabsByWs[id] || [], (t) => t.status);
  return s === "idle" ? null : s;
}

// Aggregate dot on the trigger — only for workspaces that are NOT the active one,
// so it means "something is happening where you can't see it".
const otherStatus = computed<TermStatus | null>(() => {
  const tabs = store.workspaces
    .filter((w) => w.id !== active.value?.id)
    .flatMap((w) => termTabs.tabsByWs[w.id] || []);
  const s = aggregateStatus(tabs, (t) => t.status);
  return s === "idle" ? null : s;
});

function prTone(info: PrInfo): string {
  if (info.checks === "fail") return "fail";
  if (info.checks === "pending") return "pending";
  if (info.state === "MERGED") return "merged";
  if (info.state === "CLOSED") return "closed";
  if (info.isDraft) return "draft";
  return "open";
}
function prTitle(info: PrInfo): string {
  const state = info.isDraft && info.state === "OPEN" ? "draft" : info.state.toLowerCase();
  const checks = info.checks === "none" ? "" : ` · checks ${info.checks}`;
  return `PR #${info.number} (${state})${checks}`;
}

function shortPath(p: string): string {
  const tilde = p.replace(/^\/Users\/[^/]+/, "~");
  const parts = tilde.split("/").filter(Boolean);
  return parts.length <= 2 ? tilde : "~/" + parts.slice(-2).join("/");
}

// ── context menu ─────────────────────────────────────────────────────────────
const ctx = ref<{ id: number; x: number; y: number; kind: "ws" | "wt" } | null>(null);

function openCtx(ws: Workspace, e: MouseEvent, kind: "ws" | "wt") {
  ctx.value = { id: ws.id, x: Math.min(e.clientX, window.innerWidth - 200), y: e.clientY, kind };
}
function ctxWs(): Workspace | undefined {
  return store.workspaces.find((w) => w.id === ctx.value?.id);
}
function run(fn: () => unknown) {
  const r = fn();
  ctx.value = null;
  open.value = false;
  return r;
}

async function removeWorktree() {
  const id = ctx.value?.id;
  if (id == null) return;
  try {
    await store.removeWorktree(id);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (confirm(`Could not remove worktree:\n\n${msg}\n\nForce remove (discards uncommitted changes)?`)) {
      try { await store.removeWorktree(id, true); }
      catch (e2) { alert(`Force remove failed:\n${e2 instanceof Error ? e2.message : e2}`); }
    }
  }
}

function mimeForPath(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "svg") return "image/svg+xml";
  if (ext === "ico") return "image/x-icon";
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  return "image/png";
}
async function pickIcon(id: number) {
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: "Image", extensions: ["png", "jpg", "jpeg", "svg", "ico"] }],
  });
  if (!selected || typeof selected !== "string") return;
  const b64 = await invoke<string>("read_file_base64", { path: selected });
  store.setIcon(id, `data:${mimeForPath(selected)};base64,${b64}`);
}

// Click-outside / Escape close.
function onDocClick() { open.value = false; ctx.value = null; }
function onKey(e: KeyboardEvent) { if (e.key === "Escape") onDocClick(); }
onMounted(() => {
  document.addEventListener("click", onDocClick);
  document.addEventListener("keydown", onKey);
});
onUnmounted(() => {
  document.removeEventListener("click", onDocClick);
  document.removeEventListener("keydown", onKey);
});
</script>

<style scoped>
.wsp { position: relative; padding: 6px; flex-shrink: 0; }

/* ── Trigger ───────────────────────────────────────────────────── */
.wsp-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  background: var(--bg-base);
  border: 1px solid var(--border);
  border-radius: 7px;
  cursor: pointer;
  text-align: left;
  transition: background .12s, border-color .12s;
}
.wsp-trigger:hover, .wsp-trigger.open { background: var(--bg-hover); border-color: color-mix(in srgb, var(--accent) 40%, var(--border)); }

.wsp-icon-wrap {
  width: 24px; height: 24px; flex-shrink: 0;
  border-radius: 6px; overflow: hidden;
  display: flex; align-items: center; justify-content: center;
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.wsp-icon-wrap.sm { width: 20px; height: 20px; border-radius: 5px; background: color-mix(in srgb, var(--text-muted) 10%, transparent); }
.wsp-custom-icon { width: 100%; height: 100%; object-fit: cover; }
.wsp-icon { color: var(--accent); }
.wsp-icon.wt { color: #a78bfa; }

.wsp-info { flex: 1; min-width: 0; }
.wsp-name {
  font-size: 12.5px; font-weight: 600; color: var(--text-primary);
  letter-spacing: -0.01em;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.wsp-sub {
  font-size: 9.5px; font-family: var(--font-mono); color: var(--text-muted);
  opacity: 0.7; margin-top: 1px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.wsp-caret { color: var(--text-muted); flex-shrink: 0; }

/* ── Menu ──────────────────────────────────────────────────────── */
.wsp-menu {
  position: fixed;
  z-index: 1000;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 4px;
  box-shadow: 0 14px 36px rgba(0, 0, 0, 0.55);
  max-height: 60vh;
  overflow-y: auto;
}

.wsp-row {
  display: flex; align-items: center; gap: 7px;
  padding: 5px 7px;
  border-radius: 5px;
  cursor: pointer;
  color: var(--text-secondary);
  transition: background .1s, color .1s;
}
.wsp-row:hover { background: var(--bg-hover); color: var(--text-primary); }
.wsp-row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); color: var(--text-primary); }

.wsp-wt {
  margin-left: 14px;
  padding-left: 8px;
  border-left: 1px solid color-mix(in srgb, #a78bfa 30%, transparent);
  border-radius: 0 5px 5px 0;
}

.wsp-row-name {
  flex: 1; min-width: 0;
  font-size: 11.5px; font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.wsp-count {
  flex-shrink: 0;
  min-width: 15px;
  padding: 1px 5px;
  border-radius: 7px;
  font-size: 9px;
  font-weight: 600;
  line-height: 1.5;
  text-align: center;
  color: var(--text-muted);
  background: rgba(255, 255, 255, 0.07);
}
.wsp-row.active .wsp-count { color: var(--accent); background: color-mix(in srgb, var(--accent) 15%, transparent); }
.wsp-count.other { font-family: var(--font-mono); }

.wsp-btn {
  background: none; border: none; padding: 3px; border-radius: 4px;
  color: var(--text-muted); cursor: pointer; display: none; flex-shrink: 0;
}
.wsp-row:hover .wsp-btn { display: flex; }
.wsp-btn:hover { color: #a78bfa; background: color-mix(in srgb, #a78bfa 14%, transparent); }
/* A pinned row keeps its pin visible — it's state, not an action. */
.wsp-btn.pin.on { display: flex; color: var(--accent); }
.wsp-btn.pin:hover { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }

.wsp-sep { height: 1px; background: var(--border); margin: 4px 2px; }

.wsp-action {
  display: flex; align-items: center; gap: 8px; width: 100%;
  background: none; border: none; border-radius: 5px;
  color: var(--text-secondary); cursor: pointer;
  font-size: 11.5px; font-family: var(--font-ui);
  text-align: left; padding: 6px 8px;
}
.wsp-action:hover { background: var(--bg-hover); color: var(--text-primary); }

.wsp-empty {
  font-size: 11px; color: var(--text-muted);
  text-align: center; padding: 20px 12px; line-height: 1.6;
}

/* ── PR badge (mirrors Sidebar) ────────────────────────────────── */
.pr-badge {
  flex-shrink: 0; display: inline-flex; align-items: center; gap: 3px;
  font-size: 9px; font-weight: 600; font-family: var(--font-mono); line-height: 1;
  padding: 1px 5px 1px 4px; border-radius: 7px;
  color: var(--text-muted); background: rgba(255, 255, 255, 0.06);
}
.pr-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; background: var(--text-muted); }
.pr-open    { color: #4ade80; background: color-mix(in srgb, #4ade80 12%, transparent); }
.pr-open    .pr-dot { background: #4ade80; }
.pr-draft   { color: #9ca3af; }
.pr-draft   .pr-dot { background: #9ca3af; }
.pr-merged  { color: #a78bfa; background: color-mix(in srgb, #a78bfa 14%, transparent); }
.pr-merged  .pr-dot { background: #a78bfa; }
.pr-closed  { color: #f87171; background: color-mix(in srgb, #f87171 12%, transparent); }
.pr-closed  .pr-dot { background: #f87171; }
.pr-fail    { color: #f87171; background: color-mix(in srgb, #f87171 14%, transparent); }
.pr-fail    .pr-dot { background: #f87171; animation: pr-pulse 1.6s ease-in-out infinite; }
.pr-pending { color: #fbbf24; background: color-mix(in srgb, #fbbf24 14%, transparent); }
.pr-pending .pr-dot { background: #fbbf24; animation: pr-pulse 1.6s ease-in-out infinite; }
@keyframes pr-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.55; } }

/* ── Context menu ──────────────────────────────────────────────── */
.ctx-menu {
  position: fixed; z-index: 1000; min-width: 170px;
  background: var(--bg-panel); border: 1px solid var(--border);
  border-radius: 7px; padding: 4px;
  display: flex; flex-direction: column; gap: 1px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
}
.ctx-item {
  display: flex; align-items: center; gap: 8px; width: 100%;
  background: none; border: none; border-radius: 4px;
  color: var(--text-secondary); cursor: pointer;
  font-size: 12px; font-family: var(--font-ui);
  text-align: left; padding: 6px 10px;
}
.ctx-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.ctx-item.ctx-danger:hover { color: var(--red); }
.ctx-sep { height: 1px; background: var(--border); margin: 3px 0; }
</style>
