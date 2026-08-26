<script setup lang="ts">
import { type HTMLAttributes } from "vue";
import { cn } from "@/lib/utils";

const props = defineProps<{ class?: HTMLAttributes["class"] }>();
const model = defineModel<string | number>();
</script>

<template>
  <input
    v-model="model"
    :class="
      cn(
        'flex h-8 w-full rounded-md border border-border bg-panel px-2.5 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>
