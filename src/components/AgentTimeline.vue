<template>
  <div class="relative w-full select-none" ref="wrap">
    <div v-if="!turns.length" class="p-4 text-center text-[11px] text-muted-foreground">No agent turns recorded yet.</div>
    <template v-else>
      <div class="flex items-center gap-1.5 border-b border-border px-1.5 py-0.5">
        <button class="rounded-[3px] border border-border bg-transparent px-1.5 py-0.5 font-sans text-[10px] font-medium text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="autoFit" title="Fit all turns in view">Fit</button>
        <span class="font-mono text-[10px] text-muted-foreground">{{ rangeLabel }}</span>
      </div>
      <svg
        :width="svgW" :height="svgH"
        class="tl-svg block w-full cursor-grab overflow-visible active:cursor-grabbing"
        @mousedown="onMouseDown"
        @mousemove="onMouseMove"
        @mouseup="onMouseUp"
        @mouseleave="onMouseLeave"
        @wheel.prevent="onWheel"
      >
        <!-- clip path so bars don't spill over labels -->
        <defs>
          <clipPath id="tl-chart-clip">
            <rect :x="LABEL_W" y="0" :width="chartW" :height="svgH" />
          </clipPath>
        </defs>

        <!-- Axis ticks -->
        <g class="tl-axis" :transform="`translate(0, ${AXIS_H - 1})`">
          <line :x1="LABEL_W" y1="0" :x2="svgW" y2="0" class="tl-axis-line" />
          <g v-for="tick in ticks" :key="tick.ms">
            <line
              :x1="tick.x" y1="0" :x2="tick.x" :y2="svgH - AXIS_H + 2"
              class="tl-tick-line"
            />
            <text :x="tick.x" y="-4" class="tl-tick-label" text-anchor="middle">
              {{ tick.label }}
            </text>
          </g>
        </g>

        <!-- Turn rows -->
        <g clip-path="url(#tl-chart-clip)">
          <g
            v-for="(turn, i) in reversedTurns"
            :key="turn.id"
            :transform="`translate(0, ${AXIS_H + i * ROW_H})`"
          >
            <!-- row background track -->
            <rect
              :x="LABEL_W" y="3" :width="chartW" :height="ROW_H - 6"
              class="tl-track"
            />
            <!-- segments -->
            <rect
              v-for="seg in turn.segments"
              :key="seg.start"
              :x="Math.max(LABEL_W, tx(seg.start))"
              y="3"
              :width="Math.max(0, tx(seg.end ?? now) - Math.max(LABEL_W, tx(seg.start)))"
              :height="ROW_H - 6"
              :class="`tl-seg tl-seg-${seg.state}`"
              @mouseenter="showTip($event, turn, seg)"
              @mouseleave="hideTip"
            />
          </g>
        </g>

        <!-- Row labels (left side, outside clip) -->
        <g v-for="(turn, i) in reversedTurns" :key="`lbl-${turn.id}`">
          <text
            :x="LABEL_W - 6"
            :y="AXIS_H + i * ROW_H + ROW_H / 2 + 3"
            class="tl-row-label"
            text-anchor="end"
          >T{{ turn.id + 1 }}</text>
        </g>
      </svg>

      <!-- Tooltip overlay -->
      <div
        v-if="tip"
        class="tl-tip pointer-events-none absolute z-10 whitespace-nowrap rounded-md border border-border p-1.5 text-[11px] leading-relaxed text-secondary-foreground shadow-[0_4px_12px_rgba(0,0,0,0.4)]"
        :style="{ left: tip.x + 'px', top: tip.y + 'px', background: 'color-mix(in srgb, var(--bg-panel) 95%, black)' }"
      >
        <div class="tl-tip-state text-[11px] font-semibold" :class="`tl-tip-${tip.state}`">{{ tip.state }}</div>
        <div class="text-[10px] text-muted-foreground">{{ tip.duration }}</div>
        <div v-if="tip.turnDur" class="text-[10px] text-muted-foreground">turn: {{ tip.turnDur }}</div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from "vue";
import { useAgentHistoryStore } from "@/stores/agentHistory";
import type { AgentTurn, TurnSegment } from "@/stores/agentHistory";

const props = defineProps<{ ptyId: number }>();
const store = useAgentHistoryStore();

const LABEL_W = 32;
const ROW_H = 14;
const AXIS_H = 18;
const PAD = 2;

const wrap = ref<HTMLElement>();
const svgW = ref(300);
const chartW = computed(() => Math.max(0, svgW.value - LABEL_W));

const turns = computed(() => store.getTimeline(props.ptyId));
const reversedTurns = computed(() => [...turns.value].reverse());
const svgH = computed(() => AXIS_H + reversedTurns.value.length * ROW_H + PAD);

// Live "now" ticks every second while a turn is in progress
const now = ref(Date.now());
let nowTimer: ReturnType<typeof setInterval> | undefined;
watch(
  () => store.getTimeline(props.ptyId).some((t) => !t.end),
  (live) => {
    clearInterval(nowTimer);
    if (live) nowTimer = setInterval(() => { now.value = Date.now(); }, 1000);
  },
  { immediate: true },
);

// ── Time window ─────────────────────────────────────────────────────────────
const viewStart = ref(0);
const viewDuration = ref(60_000);
let userInteracted = false;

function autoFit() {
  const t = turns.value;
  if (!t.length) return;
  const lo = t[0].start;
  const hi = Math.max(now.value, t[t.length - 1].end ?? now.value);
  const span = hi - lo || 5_000;
  viewStart.value = lo - span * 0.05;
  viewDuration.value = span * 1.1;
  userInteracted = false;
}

watch(turns, () => { if (!userInteracted) autoFit(); }, { immediate: true });

// time ms → svg x pixel
function tx(ms: number): number {
  return LABEL_W + ((ms - viewStart.value) / viewDuration.value) * chartW.value;
}

// ── Tick generation ──────────────────────────────────────────────────────────
const NICE_INTERVALS = [
  500, 1_000, 2_000, 5_000, 10_000, 30_000, 60_000,
  120_000, 300_000, 600_000, 1_800_000, 3_600_000,
];

function fmtMs(ms: number): string {
  if (ms < 1_000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1_000).toFixed(ms < 10_000 ? 1 : 0)}s`;
  return `${Math.floor(ms / 60_000)}m${Math.floor((ms % 60_000) / 1_000)}s`;
}

function fmtTick(ms: number, interval: number): string {
  const rel = ms - viewStart.value;
  if (interval < 60_000) return `+${(rel / 1_000).toFixed(rel < 10_000 ? 1 : 0)}s`;
  return `+${Math.round(rel / 60_000)}m`;
}

const ticks = computed(() => {
  const minGapPx = 60;
  const targetCount = chartW.value / minGapPx;
  const rawInterval = viewDuration.value / targetCount;
  const interval = NICE_INTERVALS.find((n) => n >= rawInterval) ?? NICE_INTERVALS[NICE_INTERVALS.length - 1];
  const viewEnd = viewStart.value + viewDuration.value;
  const first = Math.ceil(viewStart.value / interval) * interval;
  const result: { ms: number; x: number; label: string }[] = [];
  for (let ms = first; ms < viewEnd; ms += interval) {
    result.push({ ms, x: tx(ms), label: fmtTick(ms, interval) });
  }
  return result;
});

const rangeLabel = computed(() => fmtMs(viewDuration.value));

// ── Pan & zoom ───────────────────────────────────────────────────────────────
let dragging = false;
let dragStartX = 0;
let dragViewStart = 0;

function onMouseDown(e: MouseEvent) {
  dragging = true;
  dragStartX = e.clientX;
  dragViewStart = viewStart.value;
}

function onMouseMove(e: MouseEvent) {
  if (!dragging) return;
  userInteracted = true;
  const dx = e.clientX - dragStartX;
  const msPerPx = viewDuration.value / chartW.value;
  viewStart.value = dragViewStart - dx * msPerPx;
}

function onMouseUp() { dragging = false; }
function onMouseLeave() { dragging = false; hideTip(); }

function onWheel(e: WheelEvent) {
  userInteracted = true;
  const factor = e.deltaY > 0 ? 1.18 : 0.85;
  const rect = wrap.value?.getBoundingClientRect();
  const ratio = rect ? Math.max(0, (e.clientX - rect.left - LABEL_W) / chartW.value) : 0.5;
  const pivot = viewStart.value + ratio * viewDuration.value;
  viewDuration.value = Math.max(1_000, Math.min(86_400_000, viewDuration.value * factor));
  viewStart.value = pivot - ratio * viewDuration.value;
}

// ── Tooltip ──────────────────────────────────────────────────────────────────
const tip = ref<{ x: number; y: number; state: string; duration: string; turnDur?: string } | null>(null);

function showTip(e: MouseEvent, turn: AgentTurn, seg: TurnSegment) {
  const rect = wrap.value?.getBoundingClientRect();
  const segDur = (seg.end ?? now.value) - seg.start;
  const turnDur = turn.end ? turn.end - turn.start : undefined;
  tip.value = {
    x: (e.clientX - (rect?.left ?? 0)) + 12,
    y: (e.clientY - (rect?.top ?? 0)) - 8,
    state: seg.state,
    duration: fmtMs(segDur),
    turnDur: turnDur ? fmtMs(turnDur) : undefined,
  };
}
function hideTip() { tip.value = null; }

// ── Resize ───────────────────────────────────────────────────────────────────
let ro: ResizeObserver;
onMounted(() => {
  ro = new ResizeObserver(([entry]) => {
    svgW.value = entry.contentRect.width;
  });
  if (wrap.value) ro.observe(wrap.value);
});
onUnmounted(() => {
  ro?.disconnect();
  clearInterval(nowTimer);
});
</script>

<style scoped>
/* SVG presentation-attribute styling (fill/stroke) and status-color
   variants — naturally CSS-class-based for SVG, left as-is rather than
   force-fit into Tailwind's fill-*/stroke-* utilities for marginal gain. */
.tl-axis-line {
  stroke: var(--border);
  stroke-width: 1;
}

.tl-tick-line {
  stroke: var(--border);
  stroke-width: 1;
  stroke-dasharray: 3 3;
  opacity: 0.5;
}

.tl-tick-label {
  font-size: 9px;
  fill: var(--text-muted);
  font-family: var(--font-mono, monospace);
}

.tl-track {
  fill: color-mix(in srgb, var(--border) 30%, transparent);
  rx: 3;
}

.tl-row-label {
  font-size: 9px;
  fill: var(--text-muted);
  font-family: var(--font-mono, monospace);
}

.tl-seg {
  rx: 2;
  opacity: 0.85;
  transition: opacity 0.1s;
}
.tl-seg:hover { opacity: 1; }

.tl-seg-running   { fill: var(--status-running,    #fb923c); }
.tl-seg-waiting   { fill: var(--status-waiting,    #3b82f6); }
.tl-seg-permission{ fill: var(--status-permission, #f59e0b); }
.tl-seg-error     { fill: var(--status-error,      #ef4444); }

/* Tooltip status-color variants (layout is Tailwind classes in template) */
.tl-tip-running    { color: var(--status-running,    #fb923c); }
.tl-tip-waiting    { color: var(--status-waiting,    #3b82f6); }
.tl-tip-permission { color: var(--status-permission, #f59e0b); }
.tl-tip-error      { color: var(--status-error,      #ef4444); }
</style>
