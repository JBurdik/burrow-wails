<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { TabsList, type TabsListProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<TabsListProps & { class?: HTMLAttributes["class"] }>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <TabsList v-bind="delegated" :class="cn('inline-flex items-center gap-1 rounded-md border border-border bg-panel p-1', props.class)">
    <slot />
  </TabsList>
</template>
