<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { DropdownMenuItem, type DropdownMenuItemProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<DropdownMenuItemProps & { class?: HTMLAttributes["class"] }>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <DropdownMenuItem
    v-bind="delegated"
    :class="
      cn(
        'relative flex cursor-pointer select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
        props.class,
      )
    "
  >
    <slot />
  </DropdownMenuItem>
</template>
