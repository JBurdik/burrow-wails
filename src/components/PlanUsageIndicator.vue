<template>
  <!-- Subscription usage (Claude / Codex): ring = the highest limit, card = every row. -->
  <HoverCardRoot v-if="usage && anyShown" :open-delay="120" :close-delay="120">
    <HoverCardTrigger as-child>
      <button class="plan-ring" :class="peak === null ? 'plan-none' : `plan-${usageLevel(peak)}`" aria-label="Plan usage">
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5 -rotate-90">
          <circle cx="8" cy="8" :r="R" fill="none" stroke="currentColor" stroke-width="2" class="opacity-20" />
          <circle
            v-if="peak !== null"
            cx="8" cy="8" :r="R" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
            :stroke-dasharray="`${(Math.max(peak, 2) / 100) * C} ${C}`"
          />
        </svg>
        <span class="font-mono text-[10px] opacity-75">{{ peak === null ? "–" : `${Math.round(peak)}%` }}</span>
      </button>
    </HoverCardTrigger>
    <HoverCardPortal>
      <HoverCardContent
        side="top"
        align="start"
        :side-offset="6"
        class="z-50 w-[260px] rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md"
      >
        <div v-for="(p, i) in shown" :key="p.kind" :class="i > 0 && 'mt-3 border-t border-border pt-3'">
          <div class="flex items-baseline justify-between gap-2">
            <span class="text-[11px] font-medium">{{ providerName(p.kind) }}</span>
            <span v-if="p.plan" class="text-[10px] capitalize text-muted-foreground">{{ p.plan }}</span>
          </div>
          <p v-if="p.status !== 'ok'" class="mt-1 text-[10px] leading-snug text-muted-foreground">{{ p.reason || "Unavailable" }}</p>
          <p v-else-if="!p.rows.length" class="mt-1 text-[10px] text-muted-foreground">No limits reported</p>
          <div v-for="r in p.rows" :key="r.label" class="mt-2">
            <div class="flex items-baseline justify-between gap-2">
              <span class="truncate text-[10px] text-muted-foreground">{{ r.label }}</span>
              <span class="whitespace-nowrap font-mono text-[10px]">
                {{ Math.round(r.percent) }}%<span v-if="r.resets_at" class="opacity-50"> · resets {{ formatReset(r.resets_at, now) }}</span>
              </span>
            </div>
            <div class="mt-1 h-1 overflow-hidden rounded-full bg-hover">
              <div class="h-full rounded-full" :class="`plan-bar-${usageLevel(r.percent)}`" :style="{ width: r.percent + '%' }" />
            </div>
          </div>
          <p v-if="p.extra" class="mt-2 text-[10px] text-muted-foreground">{{ p.extra }}</p>
        </div>
      </HoverCardContent>
    </HoverCardPortal>
  </HoverCardRoot>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { HoverCardRoot, HoverCardTrigger, HoverCardPortal, HoverCardContent } from "reka-ui";
import { invoke } from "@tauri-apps/api/core";
import { formatReset, peakPercent, providerName, usageLevel, type PlanUsage } from "@/lib/planUsage";

const R = 6;
const C = 2 * Math.PI * R;
const POLL_MS = 60_000;

const usage = ref<PlanUsage | null>(null);
const now = ref(Date.now());

// A provider that doesn't apply (not installed / not logged in / API key) is left out.
const shown = computed(() => (usage.value?.providers ?? []).filter((p) => p.status !== "not_applicable"));
const anyShown = computed(() => shown.value.length > 0);
const peak = computed(() => peakPercent(usage.value));

let timer: ReturnType<typeof setInterval> | undefined;
let inFlight = false;

async function refresh() {
  if (inFlight || document.visibilityState !== "visible") return;
  inFlight = true;
  try {
    usage.value = await invoke<PlanUsage>("plan_usage");
    now.value = Date.now();
  } catch {
    // Keep the last good value; the next tick retries.
  } finally {
    inFlight = false;
  }
}

function onVisibility() {
  if (document.visibilityState === "visible") refresh();
}

onMounted(() => {
  refresh();
  timer = setInterval(refresh, POLL_MS);
  document.addEventListener("visibilitychange", onVisibility);
});
onUnmounted(() => {
  clearInterval(timer);
  document.removeEventListener("visibilitychange", onVisibility);
});
</script>

<style scoped>
/* Same look as AgentChat's context ring. */
.plan-ring {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 5px 2px 3px;
  border-radius: 6px;
  color: var(--text-muted);
  transition: background 120ms, color 120ms;
}
.plan-ring:hover { background: var(--bg-hover); }
.plan-ok { color: color-mix(in srgb, var(--accent) 75%, var(--text-muted)); }
.plan-warning { color: var(--yellow); }
.plan-exceeded { color: var(--red); }
.plan-bar-ok { background: color-mix(in srgb, var(--accent) 75%, transparent); }
.plan-bar-warning { background: var(--yellow); }
.plan-bar-exceeded { background: var(--red); }
</style>
