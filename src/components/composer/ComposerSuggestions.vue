<template>
  <div v-if="items.length > 0" ref="listEl" class="composer-suggestions">
    <div
      v-for="(s, i) in items"
      :key="s.key"
      class="composer-suggestion"
      :class="{ 'composer-suggestion-active': i === activeIndex }"
      @mousedown.prevent="emit('pick', s)"
    >
      <span class="composer-suggestion-token">{{ s.label }}</span>
      <span class="composer-suggestion-hint">{{ s.hint }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, useTemplateRef, watch } from "vue";
import type { ComposerSuggestion } from "@/lib/composerCompletion";

const props = defineProps<{ items: ComposerSuggestion[]; activeIndex: number }>();
const emit = defineEmits<{ pick: [suggestion: ComposerSuggestion] }>();

const listEl = useTemplateRef<HTMLElement>("listEl");

// Keyboard navigation can walk past the 200px viewport — the chat composer used
// to try this against a `.cmd-suggestion` class that no longer existed, so it
// silently did nothing.
watch(() => props.activeIndex, (i) => {
  nextTick(() => {
    listEl.value?.querySelectorAll(".composer-suggestion")[i]?.scrollIntoView({ block: "nearest" });
  });
});
</script>
