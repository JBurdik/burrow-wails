<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { cn } from "@/lib/utils";
import { type ButtonVariants, buttonVariants } from ".";

interface Props {
  variant?: ButtonVariants["variant"];
  size?: ButtonVariants["size"];
  class?: HTMLAttributes["class"];
  as?: string;
}

const props = withDefaults(defineProps<Props>(), { as: "button" });
const classes = computed(() => cn(buttonVariants({ variant: props.variant, size: props.size }), props.class));
</script>

<template>
  <component :is="as" :class="classes">
    <slot />
  </component>
</template>
