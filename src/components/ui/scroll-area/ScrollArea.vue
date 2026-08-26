<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { ScrollAreaCorner, ScrollAreaRoot, type ScrollAreaRootProps, ScrollAreaScrollbar, ScrollAreaThumb, ScrollAreaViewport } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<ScrollAreaRootProps & { class?: HTMLAttributes["class"] }>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <ScrollAreaRoot v-bind="delegated" :class="cn('relative overflow-hidden', props.class)">
    <ScrollAreaViewport class="h-full w-full">
      <slot />
    </ScrollAreaViewport>
    <ScrollAreaScrollbar orientation="vertical" class="flex w-2 touch-none select-none p-0.5">
      <ScrollAreaThumb class="relative flex-1 rounded-full bg-border" />
    </ScrollAreaScrollbar>
    <ScrollAreaCorner />
  </ScrollAreaRoot>
</template>
