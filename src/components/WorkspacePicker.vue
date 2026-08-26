<template>
  <div class="relative shrink-0 p-1.5" ref="rootEl">
    <button
      class="flex w-full items-center gap-2 rounded-[7px] border border-border bg-base px-2 py-1.5 text-left transition-colors hover:bg-hover hover:border-accent/40"
      :class="{ 'bg-hover border-accent/40': open }"
      @click.stop="toggle"
    >
      <div class="flex h-6 w-6 shrink-0 items-center justify-center overflow-hidden rounded-md bg-accent/14">
        <img v-if="active && store.icons[active.id]" :src="store.icons[active.id]" class="h-full w-full object-cover" />
        <PhGitBranch v-else-if="active?.parent_id" :size="14" class="text-purple-400" />
        <PhFolder v-else :size="14" weight="fill" class="text-accent" />
      </div>
      <div class="min-w-0 flex-1">
        <!-- A worktree's `name` is usually the repo's, so lead with its branch. -->
        <div class="truncate text-[12.5px] font-semibold tracking-[-0.01em] text-foreground">{{ active ? (active.worktree_branch || active.name) : "No workspace" }}</div>
        <div v-if="active" class="truncate font-mono text-[9.5px] text-muted-foreground opacity-70">{{ shortPath(active.path) }}</div>
      </div>
      <span v-if="otherTabs" class="shrink-0 rounded-full bg-white/7 px-1.5 py-px font-mono text-[9px] font-semibold leading-[1.5] text-muted-foreground" :title="`${otherTabs} tabs in other workspaces`">+{{ otherTabs }}</span>
      <span v-if="otherStatus" class="status-dot" :class="`status-${otherStatus}`" title="Activity in other workspaces">{{ otherStatus === 'running' ? spinnerFrame : '' }}</span>
      <PhCaretDown :size="12" weight="bold" class="shrink-0 text-muted-foreground" />
    </button>

    <!-- Teleported: .sidebar is overflow:hidden, which clipped the menu and let
         clicks fall through to the tab list underneath. -->
    <Teleport to="body">
    <div v-if="open" class="fixed z-[1000] max-h-[60vh] overflow-y-auto rounded-lg border border-border bg-panel p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="menuStyle" @click.stop>
      <div v-if="store.topLevel.length === 0" class="p-5 px-3 text-center text-[11px] leading-relaxed text-muted-foreground">
        No workspaces.<br />Open a folder to start.
      </div>

      <template v-for="ws in store.topLevel" :key="ws.id">
        <div
          class="group flex items-center gap-1.5 rounded-[5px] px-1.5 py-1 text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="{ 'bg-accent/10 text-foreground': active?.id === ws.id }"
          @click="select(ws)"
          @contextmenu.prevent.stop="openCtx(ws, $event, 'ws')"
        >
          <div class="flex h-5 w-5 shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-white/10">
            <img v-if="store.icons[ws.id]" :src="store.icons[ws.id]" class="h-full w-full object-cover" />
            <PhFolder v-else :size="13" weight="fill" class="text-accent" />
          </div>
          <span class="min-w-0 flex-1 truncate text-[11.5px] font-medium">{{ ws.name }}</span>
          <span v-if="tabCount(ws.id)" class="wsp-count shrink-0 min-w-[15px] rounded-full bg-white/7 px-1.5 py-px text-center text-[9px] font-semibold leading-[1.5] text-muted-foreground" :class="{ '!text-accent !bg-accent/15': active?.id === ws.id }" :title="`${tabCount(ws.id)} tabs`">{{ tabCount(ws.id) }}</span>
          <span
            v-if="git.prByWs[ws.id]"
            class="pr-badge"
            :class="`pr-${prTone(git.prByWs[ws.id]!)}`"
            :title="prTitle(git.prByWs[ws.id]!)"
          ><span class="pr-dot" />#{{ git.prByWs[ws.id]!.number }}</span>
          <span v-if="aggStatus(ws.id)" class="status-dot" :class="`status-${aggStatus(ws.id)}`">{{ aggStatus(ws.id) === 'running' ? spinnerFrame : '' }}</span>
          <button class="hidden shrink-0 rounded p-0.5 text-muted-foreground hover:bg-purple-400/14 hover:text-purple-400 group-hover:flex" title="New worktree" @click.stop="newWorktree(ws)"><PhGitBranch :size="12" /></button>
          <button
            class="hidden shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent/14 hover:text-accent group-hover:flex"
            :class="{ '!flex text-accent': isPinned(ws.id) }"
            :title="isPinned(ws.id) ? 'Unpin from sidebar' : `Pin to sidebar (max ${MAX_PINNED})`"
            @click.stop="togglePin(ws.id)"
          ><PhPushPin :size="12" :weight="isPinned(ws.id) ? 'fill' : 'regular'" /></button>
        </div>

        <div
          v-for="wt in store.worktreesByParent[ws.id] || []"
          :key="wt.id"
          class="group ml-3.5 flex items-center gap-1.5 rounded-r-[5px] border-l border-purple-400/30 py-1 pl-2 pr-1.5 text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="{ 'bg-accent/10 text-foreground': active?.id === wt.id }"
          :title="wt.path"
          @click="select(wt)"
          @contextmenu.prevent.stop="openCtx(wt, $event, 'wt')"
        >
          <PhGitBranch :size="12" class="text-purple-400" />
          <span class="min-w-0 flex-1 truncate text-[11.5px] font-medium">{{ wt.worktree_branch || wt.name }}</span>
          <span v-if="tabCount(wt.id)" class="shrink-0 min-w-[15px] rounded-full bg-white/7 px-1.5 py-px text-center text-[9px] font-semibold leading-[1.5] text-muted-foreground" :class="{ '!text-accent !bg-accent/15': active?.id === wt.id }" :title="`${tabCount(wt.id)} tabs`">{{ tabCount(wt.id) }}</span>
          <span
            v-if="git.prByWs[wt.id]"
            class="pr-badge"
            :class="`pr-${prTone(git.prByWs[wt.id]!)}`"
            :title="prTitle(git.prByWs[wt.id]!)"
          ><span class="pr-dot" />#{{ git.prByWs[wt.id]!.number }}</span>
          <span v-if="aggStatus(wt.id)" class="status-dot" :class="`status-${aggStatus(wt.id)}`">{{ aggStatus(wt.id) === 'running' ? spinnerFrame : '' }}</span>
          <button
            class="hidden shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent/14 hover:text-accent group-hover:flex"
            :class="{ '!flex text-accent': isPinned(wt.id) }"
            :title="isPinned(wt.id) ? 'Unpin from sidebar' : `Pin to sidebar (max ${MAX_PINNED})`"
            @click.stop="togglePin(wt.id)"
          ><PhPushPin :size="12" :weight="isPinned(wt.id) ? 'fill' : 'regular'" /></button>
        </div>
      </template>

      <div class="my-1 mx-0.5 h-px bg-border" />
      <button class="flex w-full items-center gap-2 rounded-[5px] border-0 bg-transparent px-2 py-1.5 text-left font-sans text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground" @click="open = false; emit('pick-folder')">
        <PhFolderPlus :size="13" />Open folder…
      </button>
    </div>
    </Teleport>

    <!-- Context menu -->
    <Teleport to="body">
      <div
        v-if="ctx"
        class="fixed z-[1000] flex min-w-[170px] flex-col gap-px rounded-[7px] border border-border bg-panel p-1 shadow-[0_12px_32px_rgba(0,0,0,0.5)]"
        :style="{ left: ctx.x + 'px', top: ctx.y + 'px' }"
        @click.stop
        @contextmenu.prevent.stop
      >
        <template v-if="ctx.kind === 'ws'">
          <button class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-foreground" @click="run(() => emit('rename', ctxWs()!))"><PhPencilSimple :size="13" />Rename…</button>
          <button class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-foreground" @click="run(() => pickIcon(ctx!.id))"><PhImage :size="13" />Change icon…</button>
          <button v-if="store.icons[ctx.id]" class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-foreground" @click="run(() => store.clearIcon(ctx!.id))"><PhImage :size="13" />Reset icon</button>
          <button class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-foreground" @click="run(() => ui.openBoard(ctx!.id))"><PhKanban :size="13" />Board</button>
          <div class="my-0.5 h-px bg-border" />
          <button class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-destructive" @click="run(() => store.remove(ctx!.id))"><PhTrash :size="13" />Remove</button>
        </template>
        <template v-else>
          <button class="flex w-full items-center gap-2 rounded border-0 bg-transparent px-2.5 py-1.5 text-left font-sans text-xs text-secondary-foreground hover:bg-hover hover:text-destructive" @click="run(removeWorktree)"><PhTrash :size="13" />Remove worktree</button>
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
/* PR-state badge: many distinct semantic colors + a pulse animation for
   fail/pending — kept as CSS classes rather than force-fit into Tailwind. */
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
</style>
