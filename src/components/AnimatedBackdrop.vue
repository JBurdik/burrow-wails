<template>
  <canvas ref="canvas" class="animated-backdrop" :class="{ contained }" aria-hidden="true" />
</template>

<script setup lang="ts">
// Moving backdrop for themes with `animated` (themes.ts). A canvas at
// z-index -1 inside its stacking context: above that context's own (opaque)
// background, under every surface — so it shows exactly as much as the
// surfaces' translucent bgs let through (ui.ts applyTheme makes them so).
// Full-window by default; `contained` fills the nearest positioned ancestor
// (Settings is a fixed overlay with its own copy).
// Cheap on purpose: capped frame rate, stopped while the window is hidden,
// one settled frame under prefers-reduced-motion.
import { onBeforeUnmount, onMounted, ref } from "vue";
import type { AnimatedKind } from "@/themes";

const props = defineProps<{ kind: AnimatedKind; color: string; floor: string; contained?: boolean }>();

const canvas = ref<HTMLCanvasElement | null>(null);
let ctx: CanvasRenderingContext2D | null = null;
let w = 0;
let h = 0;
let raf = 0;
let last = 0;

// ---- kinds -----------------------------------------------------------------
// Each kind: init(w, h) builds its particles, step() draws one frame.
interface Kind { fps: number; init(): void; step(c: CanvasRenderingContext2D): void }

const rand = (a: number, b: number) => a + Math.random() * (b - a);

function withAlpha(hex: string, a: number): string {
  const n = parseInt(hex.replace("#", ""), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}

// Falling katakana columns; the trail is a translucent floor-colored fill.
function matrixKind(): Kind {
  const GLYPHS = "ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789Z:.=*+-<>";
  const SIZE = 14;
  const GAP = 3; // one column every GAP glyph widths — sparse rain, less noise
  let drops: number[] = [];
  return {
    fps: 10,
    init() {
      const rows = h / SIZE;
      drops = Array.from({ length: Math.ceil(w / (SIZE * GAP)) }, (_, i) => drops[i] ?? -Math.random() * rows);
    },
    step(c) {
      c.fillStyle = withAlpha(props.floor, 0.12);
      c.fillRect(0, 0, w, h);
      c.fillStyle = props.color;
      c.font = `${SIZE}px ui-monospace, Menlo, monospace`;
      for (let i = 0; i < drops.length; i++) {
        if (drops[i] >= 0) c.fillText(GLYPHS[(Math.random() * GLYPHS.length) | 0], i * SIZE * GAP, drops[i] * SIZE);
        if (drops[i] > h / SIZE && Math.random() > 0.99) drops[i] = -Math.random() * 20;
        drops[i]++;
      }
    },
  };
}

// Twinkling stars drifting slowly left, three depth layers.
function starsKind(): Kind {
  let stars: { x: number; y: number; z: number; t: number }[] = [];
  return {
    fps: 30,
    init() {
      stars = Array.from({ length: Math.round((w * h) / 5000) }, () => ({
        x: rand(0, w), y: rand(0, h), z: rand(0.2, 1), t: rand(0, Math.PI * 2),
      }));
    },
    step(c) {
      c.clearRect(0, 0, w, h);
      c.fillStyle = props.color;
      for (const s of stars) {
        s.x -= s.z * 0.25;
        if (s.x < 0) { s.x = w; s.y = rand(0, h); }
        s.t += 0.05 * s.z;
        c.globalAlpha = (0.35 + 0.65 * (0.5 + 0.5 * Math.sin(s.t))) * s.z;
        c.beginPath();
        c.arc(s.x, s.y, s.z * 1.4, 0, Math.PI * 2);
        c.fill();
      }
      c.globalAlpha = 1;
    },
  };
}

// Bubbles rising with a sideways wobble.
function bubblesKind(): Kind {
  let bubbles: { x: number; y: number; r: number; v: number; p: number }[] = [];
  const spawn = (y = h + 20) => ({ x: rand(0, w), y, r: rand(2, 9), v: rand(0.3, 1.1), p: rand(0, Math.PI * 2) });
  return {
    fps: 30,
    init() {
      bubbles = Array.from({ length: Math.round(w / 18) }, () => spawn(rand(0, h)));
    },
    step(c) {
      c.clearRect(0, 0, w, h);
      c.strokeStyle = props.color;
      c.lineWidth = 1;
      bubbles.forEach((b, i) => {
        b.y -= b.v;
        b.p += 0.03;
        if (b.y < -20) bubbles[i] = spawn();
        c.globalAlpha = 0.25 + b.r / 18;
        c.beginPath();
        c.arc(b.x + Math.sin(b.p) * 6, b.y, b.r, 0, Math.PI * 2);
        c.stroke();
      });
      c.globalAlpha = 1;
    },
  };
}

// Stonks: emoji drifting up and to the right. To the moon.
function rocketsKind(): Kind {
  const EMOJI = ["🚀", "💎", "📈", "🐕", "💸", "🌕"];
  let items: { x: number; y: number; v: number; s: number; e: string }[] = [];
  const spawn = (y = h + 30) => ({ x: rand(-40, w), y, v: rand(0.4, 1.4), s: rand(14, 30), e: EMOJI[(Math.random() * EMOJI.length) | 0] });
  return {
    fps: 30,
    init() {
      items = Array.from({ length: Math.round(w / 60) }, () => spawn(rand(0, h)));
    },
    step(c) {
      c.clearRect(0, 0, w, h);
      c.globalAlpha = 0.35;
      items.forEach((it, i) => {
        it.y -= it.v;
        it.x += it.v * 0.5;
        if (it.y < -40 || it.x > w + 40) items[i] = spawn();
        c.font = `${it.s}px system-ui`;
        c.fillText(it.e, it.x, it.y);
      });
      c.globalAlpha = 1;
    },
  };
}

const KINDS: Record<AnimatedKind, () => Kind> = {
  matrix: matrixKind,
  stars: starsKind,
  bubbles: bubblesKind,
  rockets: rocketsKind,
};
const kind = KINDS[props.kind]();

// ---- loop ------------------------------------------------------------------
function resize() {
  const c = canvas.value;
  if (!c) return;
  const box = props.contained ? c.parentElement!.getBoundingClientRect() : { width: window.innerWidth, height: window.innerHeight };
  w = box.width;
  h = box.height;
  const dpr = window.devicePixelRatio || 1;
  c.width = Math.floor(w * dpr);
  c.height = Math.floor(h * dpr);
  ctx = c.getContext("2d");
  ctx?.setTransform(dpr, 0, 0, dpr, 0, 0);
  kind.init();
}

function loop(t: number) {
  raf = requestAnimationFrame(loop);
  if (t - last < 1000 / kind.fps || !ctx) return;
  last = t;
  kind.step(ctx);
}

function start() {
  if (raf || document.hidden) return;
  raf = requestAnimationFrame(loop);
}

function stop() {
  cancelAnimationFrame(raf);
  raf = 0;
}

function onVisibility() {
  if (document.hidden) stop();
  else start();
}

onMounted(() => {
  resize();
  window.addEventListener("resize", resize);
  if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    for (let i = 0; i < 80 && ctx; i++) kind.step(ctx); // one settled frame, no motion
    return;
  }
  document.addEventListener("visibilitychange", onVisibility);
  start();
});

onBeforeUnmount(() => {
  stop();
  window.removeEventListener("resize", resize);
  document.removeEventListener("visibilitychange", onVisibility);
});
</script>

<style scoped>
.animated-backdrop {
  position: fixed;
  inset: 0;
  width: 100vw;
  height: 100vh;
  z-index: -1;
  pointer-events: none;
}
.animated-backdrop.contained {
  position: absolute;
  width: 100%;
  height: 100%;
}
</style>
