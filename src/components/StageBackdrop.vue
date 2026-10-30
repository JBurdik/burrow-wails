<template>
  <!-- Release-channel artwork behind the sidebar header, after t3code's
       SidebarStageBackdrop: beta = night sky, dev = blueprint. Static SVG,
       colors from the --stage-* vars below (light/dark), faded out downward
       by a mask so the list stays readable. Renders nothing on stable. -->
  <div
    v-if="releaseChannel && ui.channelMarker === 'art'"
    aria-hidden="true"
    class="stage-backdrop pointer-events-none absolute inset-x-0 top-0 -z-10 h-20 select-none overflow-hidden"
    :class="ui.activeTheme.isDark && 'is-dark'"
  >
    <!-- ponytail: fixed ids, there is only ever one sidebar. -->
    <svg v-if="releaseChannel === 'beta'" class="h-full w-full" fill="none" preserveAspectRatio="xMinYMin slice" viewBox="0 0 8192 96" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="stage-night-sky" x1="24" y1="0" x2="264" y2="96" gradientUnits="userSpaceOnUse" spreadMethod="reflect">
          <stop style="stop-color: var(--stage-night-bottom)" />
          <stop offset="0.5" style="stop-color: var(--stage-night-mid)" />
          <stop offset="1" style="stop-color: var(--stage-night-top)" />
        </linearGradient>
        <radialGradient id="stage-night-glow" cx="0" cy="0" r="1" gradientTransform="translate(216 18) rotate(137) scale(120 84)" gradientUnits="userSpaceOnUse">
          <stop style="stop-color: var(--stage-night-glow-highlight)" stop-opacity="0.4" />
          <stop offset="0.5" style="stop-color: var(--stage-night-glow-secondary)" stop-opacity="0.16" />
          <stop offset="1" style="stop-color: var(--stage-night-bottom)" stop-opacity="0" />
        </radialGradient>
        <linearGradient id="stage-night-cloud" x1="0" y1="60" x2="288" y2="96" gradientUnits="userSpaceOnUse">
          <stop style="stop-color: var(--stage-night-highlight)" stop-opacity="0.5" />
          <stop offset="0.52" style="stop-color: var(--stage-night-secondary)" stop-opacity="0.62" />
          <stop offset="1" style="stop-color: var(--stage-night-tertiary)" stop-opacity="0.5" />
        </linearGradient>
        <filter id="stage-night-soft" x="-24" y="-24" width="336" height="144" filterUnits="userSpaceOnUse">
          <feGaussianBlur stdDeviation="4" />
        </filter>
        <pattern id="stage-night-stars" width="288" height="96" patternUnits="userSpaceOnUse">
          <g style="fill: var(--stage-night-line)">
            <circle v-for="s in STARS" :key="`${s.cx}-${s.cy}`" :cx="s.cx" :cy="s.cy" :r="s.r" :fill-opacity="s.opacity" />
          </g>
          <g style="stroke: var(--stage-night-sparkle)" stroke-linecap="round" stroke-opacity="0.7" stroke-width="0.6">
            <path v-for="p in SPARKLES" :key="`${p.x}-${p.y}`" :d="`M${p.x - 1.5} ${p.y}H${p.x + 1.5}M${p.x} ${p.y - 1.5}V${p.y + 1.5}`" />
          </g>
        </pattern>
        <pattern id="stage-night-glows" width="640" height="96" patternUnits="userSpaceOnUse">
          <rect width="640" height="96" fill="url(#stage-night-glow)" />
        </pattern>
      </defs>
      <rect width="100%" height="96" fill="url(#stage-night-sky)" />
      <rect width="100%" height="96" fill="url(#stage-night-glows)" />
      <rect width="100%" height="96" fill="url(#stage-night-stars)" />
      <g filter="url(#stage-night-soft)">
        <path d="M-12 88C-12 74 0 63 14 63C18 50 30 41 44 41C58 41 70 49 74 62C79 57 86 54 94 54C110 54 123 66 124 82C132 83 138 88 141 96H-12V88Z" fill="url(#stage-night-cloud)" />
      </g>
      <g filter="url(#stage-night-soft)">
        <path d="M150 96C151 84 161 75 173 75C176 64 186 57 198 57C210 57 220 64 223 75C231 75 238 80 241 87C250 87 257 91 260 96H150Z" fill="url(#stage-night-cloud)" fill-opacity="0.8" />
      </g>
    </svg>

    <svg v-else class="h-full w-full" fill="none" preserveAspectRatio="xMinYMin slice" viewBox="0 0 8192 96" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="stage-bp-paper" x1="60" y1="0" x2="220" y2="96" gradientUnits="userSpaceOnUse" spreadMethod="reflect">
          <stop style="stop-color: var(--stage-art-bottom)" />
          <stop offset="0.5" style="stop-color: var(--stage-art-mid)" />
          <stop offset="1" style="stop-color: var(--stage-art-top)" />
        </linearGradient>
        <radialGradient id="stage-bp-glow" cx="0" cy="0" r="1" gradientTransform="translate(216 14) rotate(137) scale(120 84)" gradientUnits="userSpaceOnUse">
          <stop style="stop-color: var(--stage-art-highlight)" stop-opacity="0.4" />
          <stop offset="0.52" style="stop-color: var(--stage-art-secondary)" stop-opacity="0.16" />
          <stop offset="1" style="stop-color: var(--stage-art-bottom)" stop-opacity="0" />
        </radialGradient>
        <pattern id="stage-bp-grid-minor" width="8" height="8" patternUnits="userSpaceOnUse">
          <path d="M8 0H0V8" style="stroke: var(--stage-art-grid-line)" stroke-opacity="0.14" stroke-width="0.5" />
        </pattern>
        <pattern id="stage-bp-grid-major" width="32" height="32" patternUnits="userSpaceOnUse">
          <path d="M32 0H0V32" style="stroke: var(--stage-art-grid-line)" stroke-opacity="0.26" stroke-width="0.6" />
        </pattern>
        <pattern id="stage-bp-ruler" width="32" height="6" patternUnits="userSpaceOnUse">
          <path d="M4 0V2.5M12 0V2.5M20 0V4M28 0V2.5" style="stroke: var(--stage-art-line)" stroke-opacity="0.5" stroke-width="0.5" />
        </pattern>
        <pattern id="stage-bp-glows" width="768" height="96" patternUnits="userSpaceOnUse">
          <rect width="768" height="96" fill="url(#stage-bp-glow)" />
        </pattern>
        <pattern id="stage-bp-annotations" width="768" height="96" patternUnits="userSpaceOnUse">
          <g style="stroke: var(--stage-art-line)" stroke-linecap="round" stroke-opacity="0.6" stroke-width="0.7">
            <path d="M180 64H264" stroke-dasharray="5 4" />
            <path d="M180 61V67M264 61V67" />
            <path d="M276 10V44" stroke-dasharray="4 4" stroke-opacity="0.5" />
            <path d="M273 10H279M273 44H279" stroke-opacity="0.5" />
            <path d="M348 30H428" stroke-dasharray="3.5 5" stroke-opacity="0.5" />
            <path d="M348 27V33M428 27V33" stroke-opacity="0.5" />
            <path d="M512 48V80" stroke-dasharray="5 3" stroke-opacity="0.45" />
            <path d="M509 48H515M509 80H515" stroke-opacity="0.45" />
            <path d="M590 70H724" stroke-dasharray="7 4" stroke-opacity="0.55" />
            <path d="M590 67V73M724 67V73" stroke-opacity="0.55" />
          </g>
          <g style="stroke: var(--stage-art-line)" stroke-linecap="round" stroke-opacity="0.55" stroke-width="0.6">
            <path d="M34 60L38 64M38 60L34 64M228 26H234M231 23V29M143 51H149M146 48V54M316 16L322 22M322 16L316 22M468 70H476M472 66V74M558 28L564 34M564 28L558 34M742 44H750M746 40V48" />
          </g>
          <g style="stroke: var(--stage-art-line)" stroke-opacity="0.35" stroke-width="0.6">
            <circle cx="196" cy="38" r="13" stroke-dasharray="3.5 4" />
            <path d="M196 33V43M191 38H201" stroke-opacity="0.6" stroke-width="0.4" />
            <circle cx="414" cy="64" r="10" stroke-dasharray="2.5 3.5" />
            <path d="M414 60V68M410 64H418" stroke-opacity="0.6" stroke-width="0.4" />
            <circle cx="648" cy="32" r="15" stroke-dasharray="4 5" />
            <path d="M648 26V38M642 32H654" stroke-opacity="0.6" stroke-width="0.4" />
          </g>
        </pattern>
      </defs>
      <rect width="100%" height="96" fill="url(#stage-bp-paper)" />
      <rect width="100%" height="96" fill="url(#stage-bp-glows)" />
      <rect width="100%" height="96" fill="url(#stage-bp-grid-minor)" />
      <rect width="100%" height="96" fill="url(#stage-bp-grid-major)" />
      <rect width="100%" height="6" fill="url(#stage-bp-ruler)" />
      <rect width="100%" height="96" fill="url(#stage-bp-annotations)" />
    </svg>
  </div>
</template>

<script setup lang="ts">
import { useUIStore } from "@/stores/ui";
import { releaseChannel } from "@/lib/releaseChannel";

const ui = useUIStore();

const STARS = [
  { cx: 14, cy: 10, r: 0.6, opacity: 0.85 }, { cx: 38, cy: 22, r: 0.4, opacity: 0.55 },
  { cx: 58, cy: 8, r: 0.5, opacity: 0.7 }, { cx: 84, cy: 16, r: 0.4, opacity: 0.5 },
  { cx: 104, cy: 7, r: 0.6, opacity: 0.8 }, { cx: 126, cy: 20, r: 0.4, opacity: 0.55 },
  { cx: 148, cy: 11, r: 0.5, opacity: 0.7 }, { cx: 170, cy: 24, r: 0.4, opacity: 0.5 },
  { cx: 192, cy: 9, r: 0.6, opacity: 0.8 }, { cx: 214, cy: 18, r: 0.4, opacity: 0.55 },
  { cx: 236, cy: 8, r: 0.5, opacity: 0.7 }, { cx: 258, cy: 20, r: 0.45, opacity: 0.6 },
  { cx: 278, cy: 11, r: 0.55, opacity: 0.75 }, { cx: 26, cy: 34, r: 0.4, opacity: 0.45 },
  { cx: 118, cy: 34, r: 0.4, opacity: 0.45 }, { cx: 202, cy: 32, r: 0.4, opacity: 0.5 },
  { cx: 268, cy: 34, r: 0.4, opacity: 0.45 },
];
const SPARKLES = [{ x: 70, y: 28 }, { x: 160, y: 36 }, { x: 246, y: 26 }];
</script>

<style scoped>
/* Light: pale washes with ink-colored marks, so dark sidebar text stays on a
   light ground. Dark: t3code's original night/blueprint palettes, dimmed by
   opacity so light text keeps its contrast. The mask fades into --sidebar-bg. */
.stage-backdrop {
  --stage-night-top: #d9d3f6;
  --stage-night-mid: #e2e4f8;
  --stage-night-bottom: #edf0fb;
  --stage-night-highlight: #c7d6ff;
  --stage-night-secondary: #d4c9fb;
  --stage-night-tertiary: #ead0f5;
  --stage-night-line: #4b4fc4;
  --stage-night-glow-highlight: #b9b4f5;
  --stage-night-glow-secondary: #dcd8fb;
  --stage-night-sparkle: #6d5bd6;
  --stage-art-top: #dbe7fd;
  --stage-art-mid: #e6eefd;
  --stage-art-bottom: #f0f5ff;
  --stage-art-highlight: #ffffff;
  --stage-art-secondary: #c9dcfb;
  --stage-art-line: #2f63c9;
  --stage-art-grid-line: #3a6fd6;
  opacity: 0.85;
  mask-image: linear-gradient(to bottom, black 0%, black 35%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, black 0%, black 35%, transparent 100%);
}
.stage-backdrop.is-dark {
  --stage-night-top: oklch(0.283792 0.117327 297.201);
  --stage-night-mid: oklch(0.227147 0.086086 277.99);
  --stage-night-bottom: oklch(0.200528 0.055699 261.216);
  --stage-night-highlight: oklch(0.707246 0.157418 252.091);
  --stage-night-secondary: oklch(0.600473 0.182225 277.296);
  --stage-night-tertiary: oklch(0.62583 0.210886 305.994);
  --stage-night-line: oklch(0.938794 0.029114 273.103);
  --stage-night-glow-highlight: oklch(0.553749 0.176543 271.958);
  --stage-night-glow-secondary: oklch(0.345571 0.117466 273.568);
  --stage-night-sparkle: oklch(0.880867 0.057747 269.011);
  --stage-art-top: oklch(0.581473 0.149124 256.9);
  --stage-art-mid: oklch(0.456509 0.159377 261.945);
  --stage-art-bottom: oklch(0.291327 0.136578 267.649);
  --stage-art-highlight: oklch(0.951597 0.037289 215.482);
  --stage-art-secondary: oklch(0.794668 0.12136 235.46);
  --stage-art-line: oklch(0.959666 0.029238 218.179);
  --stage-art-grid-line: oklch(0.966822 0.01757 239.99);
  opacity: 0.6;
}
</style>
