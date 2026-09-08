<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { PopoverContent, type PopoverContentProps, PopoverPortal } from "reka-ui";
import { cn } from "@/lib/utils";

// Same shape as DropdownMenuContent, different primitive on purpose: a Popover
// has no typeahead, so it can hold a text input. DropdownMenu's typeahead
// swallows the keystrokes meant for one.
const props = withDefaults(defineProps<PopoverContentProps & { class?: HTMLAttributes["class"] }>(), { sideOffset: 4 });
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <PopoverPortal>
    <PopoverContent
      v-bind="delegated"
      :class="
        cn(
          'z-50 overflow-hidden rounded-md border border-border bg-popover p-0 text-popover-foreground shadow-md',
          props.class,
        )
      "
    >
      <slot />
    </PopoverContent>
  </PopoverPortal>
</template>
