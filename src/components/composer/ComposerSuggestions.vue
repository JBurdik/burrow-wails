<template>
  <div v-if="items.length > 0" ref="listEl" class="composer-suggestions">
    <div
      v-for="(s, i) in items"
      :key="s.key"
      class="composer-suggestion"
      :class="{ 'composer-suggestion-active': i === activeIndex }"
      @mousedown.prevent="emit('pick', s)"
    >
      <span class="composer-suggestion-name">{{ s.label }}</span>
      <span class="composer-suggestion-hint">{{ s.hint }}</span>
      <span v-if="s.badge" class="composer-badge" :class="`composer-badge-${s.badge}`">
        <component :is="BADGE[s.badge].icon" :size="11" weight="bold" />
        {{ BADGE[s.badge].label }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, useTemplateRef, watch } from "vue";
import { PhUser, PhFolder, PhTerminal } from "@phosphor-icons/vue";
import type { ComposerSuggestion, SuggestionBadge } from "@/lib/composerCompletion";

const props = defineProps<{ items: ComposerSuggestion[]; activeIndex: number }>();
const emit = defineEmits<{ pick: [suggestion: ComposerSuggestion] }>();

// Where the row comes from. A skill's origin decides which one wins when the
// same name exists twice, so it is worth saying on the row rather than making
// the user guess from the name.
const BADGE: Record<SuggestionBadge, { label: string; icon: unknown }> = {
  personal: { label: "Personal Skill", icon: PhUser },
  project: { label: "Project Skill", icon: PhFolder },
  command: { label: "Built-in", icon: PhTerminal },
};

const listEl = useTemplateRef<HTMLElement>("listEl");

// Keyboard navigation can walk past the panel's viewport — the chat composer
// used to try this against a `.cmd-suggestion` class that no longer existed, so
// it silently did nothing.
watch(() => props.activeIndex, (i) => {
  nextTick(() => {
    listEl.value?.querySelectorAll(".composer-suggestion")[i]?.scrollIntoView({ block: "nearest" });
  });
});
</script>
