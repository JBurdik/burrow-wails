<template>
  <div class="h-full w-full overflow-auto px-1.5 py-1" ref="viewportEl">
    <!-- xterm sizes this to its real cols*rows pixel dimensions — wider/taller
         than the viewport is exactly what makes the outer scrollbars appear. -->
    <div class="h-fit w-fit" ref="hostEl" />
  </div>
</template>

<script setup lang="ts">
// Read-only mirror of a task's PTY, for TaskDetail's "Live view" panel. Reuses
// the float-bubble snapshot handshake (request_float_snapshot / pty-data-{id})
// so it never calls create_pty or resize_pty — the real tab (in the Sidebar)
// owns the PTY and its size; this is purely a passive viewer. No term.onData
// forwarding either: typing here does nothing, by design (ponytail: avoids
// dual-consumer input/resize races on one PTY).
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { Terminal } from "@xterm/xterm";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { attachRenderer } from "@/lib/termRenderer";
import type { ITerminalAddon } from "@xterm/xterm";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { useUIStore } from "@/stores/ui";

const props = defineProps<{ ptyId: number }>();

const ui = useUIStore();
const viewportEl = ref<HTMLElement>();
const hostEl = ref<HTMLElement>();
let term: Terminal | null = null;
let renderAddon: ITerminalAddon | null = null;
let unlistenData: UnlistenFn | null = null;
let unlistenSnap: UnlistenFn | null = null;
let unlistenGrid: UnlistenFn | null = null;
let snapTimer: ReturnType<typeof setTimeout> | null = null;
let phase: "loading" | "live" = "loading";
let liveQueue: Uint8Array[] = [];

// The grid MUST match the source PTY's cols/rows — Claude Code's interactive
// UI (status bar, box borders) uses absolute cursor positioning sized to its
// real terminal, so a mismatched grid renders garbled (tried a self-sized
// grid, then a CSS-scaled one — both made the text blurry/tiny for the same
// reason a shrunk font does). Font stays fixed and crisp; a source wider than
// this panel scrolls horizontally instead.
const FONT_SIZE = 13;
function applyGrid(cols: number, rows: number) {
  if (!term || cols <= 0 || rows <= 0) return;
  if (cols !== term.cols || rows !== term.rows) term.resize(cols, rows);
}

// The grid matches the source exactly, so its rendered box is routinely
// taller/wider than this panel — the extra is meant to be revealed by
// scrolling .tlt-viewport (plain CSS overflow), not by xterm's own internal
// scrollback (there isn't any real scrollback here, just one clipped screen).
// But xterm.js's inner `.xterm-viewport` sits on top and calls
// preventDefault() on EVERY wheel event for its own scrollback handling —
// vertical included — so the browser's native scroll on our outer container
// never fires. Intercept in the capture phase, before it reaches xterm, and
// drive the outer container's scroll ourselves.
function onWheel(e: WheelEvent) {
  if (!viewportEl.value) return;
  e.preventDefault();
  e.stopPropagation();
  viewportEl.value.scrollLeft += e.deltaX;
  viewportEl.value.scrollTop += e.deltaY;
}

async function attach() {
  if (!hostEl.value) return;
  term = new Terminal({
    theme: ui.activeTheme.xterm,
    fontFamily: ui.terminalFont,
    fontSize: FONT_SIZE,
    lineHeight: 1.4,
    cursorBlink: false,
    disableStdin: true,
    allowProposedApi: true,
    allowTransparency: true,
    scrollback: 2000,
  });
  term.loadAddon(new WebLinksAddon());
  term.open(hostEl.value);
  renderAddon = attachRenderer(term);
  viewportEl.value?.addEventListener("wheel", onWheel, { capture: true, passive: false });

  unlistenData = await listen<number[]>(`pty-data-${props.ptyId}`, (event) => {
    const bytes = new Uint8Array(event.payload);
    if (phase === "live") term?.write(bytes);
    else liveQueue.push(bytes);
  });
  unlistenSnap = await listen<{ data: string; cols: number; rows: number }>(
    `float-snap-${props.ptyId}`,
    (event) => {
      if (phase !== "loading") return;
      if (snapTimer) { clearTimeout(snapTimer); snapTimer = null; }
      applyGrid(event.payload.cols, event.payload.rows);
      term?.reset();
      term?.write(event.payload.data);
      while (liveQueue.length) term?.write(liveQueue.shift()!);
      phase = "live";
    },
  );
  // Source resized (its own tab reflowed) — follow so its next repaint lands
  // on a grid that matches.
  unlistenGrid = await listen<{ cols: number; rows: number }>(
    `float-grid-${props.ptyId}`,
    (event) => { if (phase === "live") applyGrid(event.payload.cols, event.payload.rows); },
  );

  phase = "loading";
  liveQueue = [];
  await invoke("request_float_snapshot", { ptyId: props.ptyId });
  snapTimer = setTimeout(() => {
    if (phase !== "loading") return;
    while (liveQueue.length) term?.write(liveQueue.shift()!);
    phase = "live";
  }, 400);
}

function detach() {
  viewportEl.value?.removeEventListener("wheel", onWheel, { capture: true } as EventListenerOptions);
  unlistenData?.();
  unlistenSnap?.();
  unlistenGrid?.();
  unlistenData = null;
  unlistenSnap = null;
  unlistenGrid = null;
  if (snapTimer) { clearTimeout(snapTimer); snapTimer = null; }
  renderAddon?.dispose();
  term?.dispose();
  term = null;
}

onMounted(async () => { await nextTick(); attach(); });
onBeforeUnmount(detach);

// The task's PTY isn't known until the tab spawns — reattach if it changes.
watch(() => props.ptyId, async () => {
  detach();
  await nextTick();
  attach();
});
</script>
