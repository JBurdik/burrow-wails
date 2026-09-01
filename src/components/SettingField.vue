<!-- Label + optional hint around one settings control. Every field on the
     Providers page has this shape, so it lives here instead of being repeated. -->
<template>
  <div class="flex flex-col gap-1.5">
    <div class="flex items-center gap-2">
      <span class="text-[12px] font-medium text-foreground">{{ label }}</span>
      <span v-if="$slots.action" class="ml-auto"><slot name="action" /></span>
    </div>
    <slot />
    <p v-if="hint" class="m-0 text-[11px] leading-relaxed text-muted-foreground/70">{{ hint }}</p>
  </div>
</template>

<script setup lang="ts">
defineProps<{ label: string; hint?: string }>();
</script>
