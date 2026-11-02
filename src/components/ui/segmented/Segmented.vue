<!-- Segmented control for small option sets (2–4 short labels); a Select for
     anything longer. -->
<script setup lang="ts" generic="T extends string">
defineProps<{ options: readonly { value: T; label: string }[] }>();
const model = defineModel<T>();
</script>

<template>
  <div role="radiogroup" class="inline-flex shrink-0 items-center gap-0.5 rounded-lg border border-border bg-base p-0.5">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      role="radio"
      :aria-checked="model === opt.value"
      class="h-[26px] rounded-md px-3 text-[12px] transition-colors duration-75"
      :class="model === opt.value ? 'bg-selected text-foreground' : 'text-muted-foreground hover:text-foreground'"
      @click="model = opt.value"
    >{{ opt.label }}</button>
  </div>
</template>
