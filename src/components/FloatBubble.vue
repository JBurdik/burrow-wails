<template>
  <div class="bubble-root" :class="{ expanded }">
    <!-- ── Collapsed state: a thin bar (icon · title · status · close) ── -->
    <div v-if="!expanded" class="bubble-bar" data-tauri-drag-region @click="expand" :title="displayTitle">
      <img v-if="projectIcon" :src="projectIcon" class="proj-icon" alt="" />
      <PhFolder v-else :size="13" weight="fill" class="proj-icon-glyph" />
      <PhRobot v-if="isAgentSession" :size="12" class="bar-icon" />
      <PhTerminal v-else :size="12" class="bar-icon" />
      <span class="bar-title">{{ displayTitle }}</span>
      <PhSpinner v-if="status === 'running'" :size="13" class="status-spin spin" />
      <span v-else-if="status !== 'idle'" class="status-dot" :class="`status-${status}`" />
      <button class="bar-close" title="Close" @click.stop="closeWindow">
        <PhX :size="10" weight="bold" />
      </button>
    </div>

    <!-- ── Expanded state: terminal panel ── -->
    <template v-else>
      <div class="bubble-header" data-tauri-drag-region>
        <img v-if="projectIcon" :src="projectIcon" class="proj-icon" alt="" />
        <PhFolder v-else :size="13" weight="fill" class="proj-icon-glyph" />
        <PhSpinner v-if="status === 'running'" :size="13" class="status-spin spin" />
        <span v-else-if="status !== 'idle'" class="status-dot" :class="`status-${status}`" />
        <span class="bubble-title" data-tauri-drag-region>{{ displayTitle }}</span>
        <div class="bubble-actions">
          <button class="bubble-btn" title="Focus in main window" @click="focusMain">
            <PhArrowSquareOut :size="11" />
          </button>
          <button class="bubble-btn" title="Collapse" @click="collapse">
            <PhMinus :size="11" weight="bold" />
          </button>
          <button class="bubble-btn bubble-btn-close" title="Close" @click="closeWindow">
            <PhX :size="11" weight="bold" />
          </button>
        </div>
      </div>
      <div class="bubble-term" ref="hostEl" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { Terminal } from "@xterm/xterm";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { attachRenderer } from "@/lib/termRenderer";
import type { ITerminalAddon } from "@xterm/xterm";
import { invoke } from "@tauri-apps/api/core";
import { listen, emit, type UnlistenFn } from "@tauri-apps/api/event";
import { PhArrowSquareOut, PhMinus, PhRobot, PhTerminal, PhX, PhSpinner, PhFolder } from "@phosphor-icons/vue";
import { useUIStore } from "@/stores/ui";
import { useWorkspaceStore } from "@/stores/workspace";
import { applyAgentEvent, type TermStatus, type StatusLeaf, type ReducerCtx } from "@/lib/terminalStatus";
import { playSound } from "@/lib/sounds";
import "@xterm/xterm/css/xterm.css";

const props = defineProps<{ ptyId: number; wsId: number; initTitle: string }>();

const ui = useUIStore();
const wsStore = useWorkspaceStore();
const hostEl = ref<HTMLElement>();
const displayTitle = ref(props.initTitle || `PTY ${props.ptyId}`);
const status = ref<TermStatus>("idle");
const isAgentSession = ref(false);
// Local leaf-like object fed to the shared reducer.
const _leaf: StatusLeaf = { id: props.ptyId, status: "idle", busy: false, isAgent: false };
const expanded = ref(false);

// Project (workspace) icon — read reactively from the workspace store's
// `icons` computed (SQLite-backed `icon` column, keyed by workspace id).
// Shows which project this floating terminal belongs to. Null → fall back to
// a folder glyph.
const projectIcon = computed<string | null>(() => wsStore.icons[props.wsId] ?? null);

let term: Terminal | null = null;
let renderAddon: ITerminalAddon | null = null;
let unlistenData: UnlistenFn | null = null;
let unlistenSnap: UnlistenFn | null = null;
let unlistenGrid: UnlistenFn | null = null;
let unlistenHook: UnlistenFn | null = null;
let resizeObserver: ResizeObserver | null = null;
let doneTimer: ReturnType<typeof setTimeout> | null = null;
let snapTimer: ReturnType<typeof setTimeout> | null = null;
let termMounted = false;
// Snapshot handshake state. While "loading" we buffer live pty bytes into
// liveQueue instead of writing them, so the serialized snapshot (the main
// window's exact current screen) lands first, then the queued live bytes apply
// on top in order. "idle" = collapsed (drop bytes; resync via snapshot on next
// expand). "live" = write straight through.
let phase: "idle" | "loading" | "live" = "idle";
let liveQueue: Uint8Array[] = [];
let unlistenMoved: UnlistenFn | null = null;
let unlistenResized: UnlistenFn | null = null;
let moveTimer: ReturnType<typeof setTimeout> | null = null;
let resizeWinTimer: ReturnType<typeof setTimeout> | null = null;

async function focusMain() {
  await emit("float-focus-tab", { ptyId: props.ptyId, wsId: props.wsId });
  const { WebviewWindow } = await import("@tauri-apps/api/webviewWindow");
  const main = await WebviewWindow.getByLabel("main");
  if (main) { await main.show(); await main.setFocus(); }
}

function closeWindow() {
  invoke("close_float_window", { label: `float-${props.ptyId}` }).catch(() => {});
}

function collapse() {
  expanded.value = false;
  // Stop mirroring: drop buffered/live bytes (status dots still come from the
  // independent pty-hook channel). Next expand re-syncs via a fresh snapshot.
  phase = "idle";
  liveQueue = [];
  if (snapTimer) { clearTimeout(snapTimer); snapTimer = null; }
  // The v-if swap destroys the terminal's host element; dispose the term so the
  // next expand mounts a FRESH one bound to the new host (otherwise it writes to
  // the detached old element → blank pane after re-expand).
  resizeObserver?.disconnect();
  resizeObserver = null;
  renderAddon?.dispose();
  renderAddon = null;
  term?.dispose();
  term = null;
  termMounted = false;
  invoke("set_window_size", { label: `float-${props.ptyId}`, width: 240, height: 36 }).catch(() => {});
}

async function expand() {
  expanded.value = true;
  await invoke("set_window_size", { label: `float-${props.ptyId}`, width: 620, height: 420 }).catch(() => {});
  // Wait for the expanded block to render (v-if swap) + its ref to populate,
  // THEN mount/fit so xterm measures the real 460px box (not the 64px collapsed
  // one → wrong cols).
  await nextTick();
  await nextTick();
  if (!termMounted) mountTerm();
  else term?.reset();
  // Begin the snapshot handshake: buffer live bytes, ask the main XTerm for its
  // serialized current screen. The reply (float-snap-{id}) writes the snapshot
  // then drains the queue and flips to live. Falls back to live-only on timeout.
  phase = "loading";
  liveQueue = [];
  await invoke("request_float_snapshot", { ptyId: props.ptyId });
  if (snapTimer) clearTimeout(snapTimer);
  snapTimer = setTimeout(() => {
    if (phase !== "loading") return;
    while (liveQueue.length) term?.write(liveQueue.shift()!);
    phase = "live";
  }, 400);
  // Re-fit the font a few times as the async window resize lands + focus so
  // typing works immediately.
  for (const d of [60, 180, 400]) setTimeout(() => { fitFont(); term?.focus(); }, d);
}

// Font is bound by WIDTH only. The float must keep the source's COLUMN count (so
// line wrapping matches 1:1) but NOT its row count — so a shorter window just
// shows fewer rows at the SAME big font instead of shrinking everything. Rows are
// then whatever fits the height. Monospace cell ≈ 0.6·fs wide, 1.4·fs tall.
function fitFont() {
  if (!term || !hostEl.value) return;
  const w = hostEl.value.clientWidth;
  const h = hostEl.value.clientHeight;
  if (w < 10 || h < 10) return;
  const fs = Math.max(7, Math.min(22, Math.floor(w / (term.cols * 0.6))));
  if (term.options.fontSize !== fs) term.options.fontSize = fs;
  const rows = Math.max(2, Math.floor(h / (fs * 1.4)));
  if (rows !== term.rows) term.resize(term.cols, rows);
}

// Match the float's grid to the source (cols×rows) AND size the WINDOW so that
// grid renders at a comfortable ~12px font — the source is often very wide, so a
// fixed window would force a tiny font. Aspect follows the terminal; capped to a
// sane max, and the user can still resize afterwards.
function applyGrid(cols: number, rows: number) {
  if (!term || cols <= 0) return;
  // Match WIDTH (cols) only — rows are set by fitFont to whatever the height fits.
  if (cols !== term.cols) term.resize(cols, term.rows);
  // Default window: wide enough for cols at a comfortable font, tall enough to
  // show all source rows at that font. Font is width-bound, so a taller window
  // doesn't shrink it — the user can shorten the height freely afterwards.
  const TARGET_FS = 14;
  const winW = Math.min(1100, Math.round(cols * TARGET_FS * 0.6 + 12));
  const winH = Math.min(720, Math.round((rows > 0 ? rows : 24) * TARGET_FS * 1.4 + 32 + 10));
  invoke("set_window_size", { label: `float-${props.ptyId}`, width: winW, height: winH })
    .then(() => nextTick())
    .then(() => fitFont())
    .catch(() => {});
  fitFont();
}

function mountTerm() {
  if (!hostEl.value || termMounted) return;
  termMounted = true;

  term = new Terminal({
    theme: ui.activeTheme.xterm,
    fontFamily: ui.terminalFont,
    fontSize: ui.terminalFontSize,
    lineHeight: 1.4,
    cursorBlink: true,
    cursorStyle: "bar",
    allowProposedApi: true,
    allowTransparency: true,
    scrollback: 2000,
  });
  term.loadAddon(new WebLinksAddon());
  term.open(hostEl.value);
  renderAddon = attachRenderer(term);
  fitFont();
  term.focus();

  term.onData((data) => {
    invoke("write_pty", { id: props.ptyId, data: Array.from(new TextEncoder().encode(data)) });
  });

  // Click anywhere in the pane re-focuses, so typing routes to the PTY.
  hostEl.value.addEventListener("mousedown", () => term?.focus());

  resizeObserver = new ResizeObserver(() => fitFont());
  resizeObserver.observe(hostEl.value.parentElement ?? hostEl.value);
}

onMounted(async () => {
  // Start collapsed — a thin bar (title + status), always on top
  invoke("set_window_size", { label: `float-${props.ptyId}`, width: 240, height: 36 }).catch(() => {});

  // Live mirror: piggyback on the SAME pty-data-{id} event the main window
  // consumes (Tauri app.emit broadcasts to every window — no extra daemon
  // stream). Phase gates it: loading → buffer, live → write, idle → drop.
  unlistenData = await listen<number[]>(`pty-data-${props.ptyId}`, (event) => {
    const bytes = new Uint8Array(event.payload);
    if (phase === "live") term?.write(bytes);
    else if (phase === "loading") liveQueue.push(bytes);
    // idle (collapsed): drop — we resync via snapshot on next expand
  });

  // Snapshot reply from the main XTerm: its exact current screen (serialized) +
  // its grid dims. Match the grid so the screen renders identically, then write
  // the snapshot, drain buffered live bytes, go live.
  unlistenSnap = await listen<{ data: string; cols: number; rows: number }>(
    `float-snap-${props.ptyId}`,
    (event) => {
      if (phase !== "loading") return;
      if (snapTimer) { clearTimeout(snapTimer); snapTimer = null; }
      const { data, cols, rows } = event.payload;
      // Match grid + size the window for a readable mirror.
      applyGrid(cols, rows);
      term?.reset();
      term?.write(data);
      while (liveQueue.length) term?.write(liveQueue.shift()!);
      phase = "live";
      term?.focus();
    },
  );

  // The source terminal resized → match its new grid so the live repaint (the
  // shared PTY's SIGWINCH already triggered the agent) renders correctly.
  unlistenGrid = await listen<{ cols: number; rows: number }>(
    `float-grid-${props.ptyId}`,
    (event) => {
      if (!term || phase !== "live") return;
      const { cols, rows } = event.payload;
      // Source terminal resized → match its grid AND resize the float window so
      // the mirror stays readable (not just a font tweak inside a fixed window).
      if (cols > 0 && rows > 0 && (cols !== term.cols || rows !== term.rows)) {
        applyGrid(cols, rows);
      }
    },
  );

  // Reducer context for the float: floats are always "not watching" (detached
  // mirror) so done settles to review, matching main-window away-case semantics.
  const bubbleCtx: ReducerCtx = {
    watching: false,
    setDoneTimer(_id: number) {
      if (doneTimer) clearTimeout(doneTimer);
      doneTimer = setTimeout(() => {
        if (_leaf.status === "done") { _leaf.status = "idle"; status.value = "idle"; }
      }, 4000);
    },
    clearDoneTimer(_id: number) {
      if (doneTimer) { clearTimeout(doneTimer); doneTimer = null; }
    },
    playSound(kind) { playSound(kind); },
    onSettled(_leaf) { /* floats don't fire desktop notifications or git refresh */ },
  };

  unlistenHook = await listen<string>(`pty-hook-${props.ptyId}`, (event) => {
    const s = event.payload;
    if (s === "running" || s === "waiting" || s === "permission" || s === "done") {
      if (s === "running") isAgentSession.value = true;
      _leaf.isAgent = true;
      applyAgentEvent(_leaf, s as "running" | "waiting" | "permission" | "done", bubbleCtx);
      status.value = _leaf.status;
    }
  });

  // Corner-snapping: dragging fires move events continuously; 220ms after the
  // last one (drag settled) snap the window to its nearest corner + re-stack.
  // Snapping itself moves the window → guard against an infinite snap loop with
  // a short suppression window.
  const { getCurrentWindow } = await import("@tauri-apps/api/window");
  const win = getCurrentWindow();
  let snapping = false;
  unlistenMoved = await win.onMoved(() => {
    if (snapping) return;
    if (moveTimer) clearTimeout(moveTimer);
    moveTimer = setTimeout(async () => {
      snapping = true;
      await invoke("snap_float_window", { label: `float-${props.ptyId}` }).catch(() => {});
      setTimeout(() => { snapping = false; }, 150);
    }, 220);
  });

  // Manual window resize: sync the real size into the layout + re-stack so the
  // others realign and nothing overflows the screen edge.
  unlistenResized = await win.onResized(() => {
    if (resizeWinTimer) clearTimeout(resizeWinTimer);
    resizeWinTimer = setTimeout(() => {
      invoke("sync_float_size", { label: `float-${props.ptyId}` }).catch(() => {});
    }, 200);
  });
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  unlistenData?.();
  unlistenSnap?.();
  unlistenGrid?.();
  unlistenHook?.();
  unlistenMoved?.();
  unlistenResized?.();
  if (doneTimer) clearTimeout(doneTimer);
  if (snapTimer) clearTimeout(snapTimer);
  if (moveTimer) clearTimeout(moveTimer);
  if (resizeWinTimer) clearTimeout(resizeWinTimer);
  renderAddon?.dispose();
  term?.dispose();
});

watch(() => ui.activeTheme, (t) => { if (term) term.options.theme = t.xterm; });
// Font FAMILY follows the user pref; SIZE is auto-fit to the window (fitFont).
watch(() => ui.terminalFont, (font) => {
  if (!term) return;
  term.options.fontFamily = font;
  fitFont();
});
</script>

<style>
/* The float window mounts FloatBubble directly (not App.vue), so App.vue's
   global box-sizing reset doesn't apply here — without this, `width:100%` +
   padding overflows and pushes the close button off the window edge. */
*, *::before, *::after { box-sizing: border-box; }

html, body, #app {
  width: 100%; height: 100%;
  margin: 0; padding: 0;
  overflow: hidden;
  background: transparent;
}
</style>

<style scoped>
.bubble-root {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: var(--font-ui, system-ui, sans-serif);
  /* transparent bg — OS clips shadow to border-radius */
  background: transparent;
}

/* ── Collapsed: a thin bar filling the window — icon · title · status · close ── */
.bubble-bar {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 8px;
  overflow: hidden;
  background: var(--bg-panel, #1c1c1e);
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.14);
  cursor: pointer;
  color: var(--text-secondary, #aaa);
  transition: background 0.15s, color 0.15s;
  -webkit-app-region: drag;
}
.bubble-bar:hover {
  background: var(--bg-hover, #2a2a2c);
  color: var(--text-primary, #eee);
}

.bar-icon { flex-shrink: 0; color: var(--accent, #7aa2f7); }

/* Project icon (workspace) — shows which project the terminal belongs to. */
.proj-icon {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  border-radius: 4px;
  object-fit: cover;
}
.proj-icon-glyph { flex-shrink: 0; color: var(--text-muted, #888); }

.bar-title {
  flex: 1;
  min-width: 0; /* shrink before the close button, so it never overflows */
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.bar-close {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted, #666);
  cursor: pointer;
  padding: 0;
  opacity: 0;
  transition: opacity 0.12s, background 0.12s, color 0.12s;
  -webkit-app-region: no-drag;
}
.bubble-bar:hover .bar-close { opacity: 0.7; }
.bar-close:hover { opacity: 1 !important; background: rgba(239,68,68,0.25); color: var(--red, #ef4444); }

.bubble-btn-close:hover {
  background: rgba(239,68,68,0.25);
  color: var(--red, #ef4444);
}

/* ── Expanded: fills the whole window edge-to-edge. ── */
.bubble-root.expanded {
  background: var(--bg-base, #0d0d0d);
  border: 1px solid rgba(255,255,255,0.1);
}

.bubble-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  height: 32px;
  flex-shrink: 0;
  overflow: hidden;
  background: var(--bg-panel, #111);
  border-bottom: 1px solid rgba(255,255,255,0.06);
  user-select: none;
  -webkit-app-region: drag;
}

.bubble-title {
  flex: 1;
  min-width: 0; /* let the title shrink so the action buttons never overflow */
  font-size: 11px;
  color: var(--text-secondary, #888);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  -webkit-app-region: drag;
}

.bubble-actions {
  display: flex;
  gap: 2px;
  flex-shrink: 0;
  -webkit-app-region: no-drag;
}

.bubble-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border: none;
  background: transparent;
  color: var(--text-muted, #666);
  border-radius: 3px;
  cursor: pointer;
  padding: 0;
  transition: background 0.1s, color 0.1s;
}
.bubble-btn:hover {
  background: rgba(255,255,255,0.08);
  color: var(--text-primary, #e0e0e0);
}

.bubble-term {
  flex: 1;
  overflow: hidden;
  padding: 4px;
}
.bubble-term :deep(.xterm) { height: 100%; }
.bubble-term :deep(.xterm-viewport) { background: transparent !important; }

/* Status indicator — shared by bar + header.
   running → orange loader (PhSpinner + spin),
   waiting → blue dot, done → lime dot,
   review → pulsing green dot (agent finished while away). */
.status-spin {
  flex-shrink: 0;
  color: var(--status-running, #fb923c);
}
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.status-dot {
  flex-shrink: 0;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  position: relative;
}
.status-dot.status-waiting { background: var(--status-waiting, #3b82f6); }
.status-dot.status-done    { background: var(--status-done, #84cc16); }
.status-dot.status-review {
  background: var(--status-review, #22c55e);
  box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.6);
  animation: review-pulse 1.8s ease-out infinite;
}
@keyframes review-pulse {
  0%   { box-shadow: 0 0 0 0 rgba(34, 197, 94, 0.6); }
  70%  { box-shadow: 0 0 0 5px rgba(34, 197, 94, 0); }
  100% { box-shadow: 0 0 0 0 rgba(34, 197, 94, 0); }
}
.status-dot.status-error {
  background: var(--status-error, #ef4444);
  box-shadow: 0 0 0 0 rgba(239, 68, 68, 0.7);
  animation: error-pulse 1.2s ease-out infinite;
}
@keyframes error-pulse {
  0%   { box-shadow: 0 0 0 0 rgba(239, 68, 68, 0.7); }
  70%  { box-shadow: 0 0 0 5px rgba(239, 68, 68, 0); }
  100% { box-shadow: 0 0 0 0 rgba(239, 68, 68, 0); }
}
</style>
