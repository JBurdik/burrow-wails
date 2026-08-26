<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { TabsTrigger, type TabsTriggerProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<TabsTriggerProps & { class?: HTMLAttributes["class"] }>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <TabsTrigger
    v-bind="delegated"
    :class="
      cn(
        'rounded-sm px-3 py-1 text-sm text-muted-foreground transition-colors data-[state=active]:bg-accent data-[state=active]:text-white',
        props.class,
      )
    "
  >
    <slot />
  </TabsTrigger>
</template>
