<script setup lang="ts">
import { computed } from "vue";
import { PhSpinner, PhWarningCircle } from "@phosphor-icons/vue";
import type { ChildActivitySummary } from "@/lib/terminalStatus";

const props = defineProps<{ summary: ChildActivitySummary | null }>();

const label = computed(() => {
  const summary = props.summary;
  if (!summary) return "";
  const subject = summary.count === 1 ? "1 sub-agent" : `${summary.count} sub-agents`;
  switch (summary.state) {
    case "error": return `${subject} failed`;
    case "needs-input": return summary.count === 1 ? `${subject} needs attention` : `${subject} need attention`;
    case "done-unread": return `${subject} finished, unread`;
    case "working": return `${subject} working`;
  }
});

const tone = computed(() => {
  switch (props.summary?.state) {
    case "error": return "bg-destructive/10 text-destructive";
    case "needs-input": return "bg-[color-mix(in_srgb,var(--yellow)_12%,transparent)] text-[var(--yellow)]";
    case "done-unread": return "bg-[color-mix(in_srgb,var(--blue)_12%,transparent)] text-[var(--blue)]";
    case "working": return "bg-accent/10 text-accent";
    default: return "";
  }
});
</script>

<template>
  <span
    v-if="summary"
    class="flex h-4 min-w-4 shrink-0 items-center justify-center gap-0.5 rounded px-1 text-[9px] font-semibold leading-none"
    :class="tone"
    :title="label"
    :aria-label="label"
    role="status"
  >
    <PhWarningCircle v-if="summary.state === 'error' || summary.state === 'needs-input'" :size="9" weight="fill" />
    <PhSpinner v-else-if="summary.state === 'working'" :size="9" class="animate-spin" />
    <span v-else class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
    <span>{{ summary.count }}</span>
  </span>
</template>
