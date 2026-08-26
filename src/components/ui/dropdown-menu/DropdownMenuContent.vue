<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { DropdownMenuContent, type DropdownMenuContentProps, DropdownMenuPortal } from "reka-ui";
import { cn } from "@/lib/utils";

const props = withDefaults(defineProps<DropdownMenuContentProps & { class?: HTMLAttributes["class"] }>(), { sideOffset: 4 });
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <DropdownMenuPortal>
    <DropdownMenuContent
      v-bind="delegated"
      :class="
        cn(
          'z-50 min-w-[10rem] overflow-hidden rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md',
          props.class,
        )
      "
    >
      <slot />
    </DropdownMenuContent>
  </DropdownMenuPortal>
</template>
