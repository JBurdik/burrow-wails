<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { Separator, type SeparatorProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = withDefaults(defineProps<SeparatorProps & { class?: HTMLAttributes["class"] }>(), {
  orientation: "horizontal",
});
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <Separator
    v-bind="delegated"
    :class="cn('shrink-0 bg-border', orientation === 'horizontal' ? 'h-px w-full' : 'h-full w-px', props.class)"
  />
</template>
