<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { TooltipContent, type TooltipContentProps, TooltipPortal } from "reka-ui";
import { cn } from "@/lib/utils";

const props = withDefaults(defineProps<TooltipContentProps & { class?: HTMLAttributes["class"] }>(), { sideOffset: 4 });
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <TooltipPortal>
    <TooltipContent
      v-bind="delegated"
      :class="
        cn(
          'z-50 overflow-hidden rounded-md border border-border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-md',
          props.class,
        )
      "
    >
      <slot />
    </TooltipContent>
  </TooltipPortal>
</template>
