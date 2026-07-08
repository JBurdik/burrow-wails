<template>
  <div class="tlt-root" ref="hostEl" />
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
const hostEl = ref<HTMLElement>();
let term: Terminal | null = null;
let renderAddon: ITerminalAddon | null = null;
let resizeObserver: ResizeObserver | null = null;
let unlistenData: UnlistenFn | null = null;
let unlistenSnap: UnlistenFn | null = null;
let snapTimer: ReturnType<typeof setTimeout> | null = null;
let phase: "loading" | "live" = "loading";
let liveQueue: Uint8Array[] = [];

function fitFont() {
  if (!term || !hostEl.value) return;
  const w = hostEl.value.clientWidth;
  const h = hostEl.value.clientHeight;
  if (w < 10 || h < 10) return;
  const fs = Math.max(7, Math.min(15, Math.floor(w / (term.cols * 0.6))));
  if (term.options.fontSize !== fs) term.options.fontSize = fs;
}

async function attach() {
  if (!hostEl.value) return;
  term = new Terminal({
    theme: ui.activeTheme.xterm,
    fontFamily: ui.terminalFont,
    fontSize: ui.terminalFontSize,
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
  fitFont();

  resizeObserver = new ResizeObserver(() => fitFont());
  resizeObserver.observe(hostEl.value);

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
      const { data, cols, rows } = event.payload;
      if (cols > 0 && rows > 0) term?.resize(cols, rows);
      term?.reset();
      term?.write(data);
      while (liveQueue.length) term?.write(liveQueue.shift()!);
      phase = "live";
      fitFont();
    },
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
  resizeObserver?.disconnect();
  resizeObserver = null;
  unlistenData?.();
  unlistenSnap?.();
  unlistenData = null;
  unlistenSnap = null;
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

<style scoped>
.tlt-root {
  width: 100%;
  height: 100%;
  padding: 4px 6px;
}
</style>
