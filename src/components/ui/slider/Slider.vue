<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import {
  SliderRange,
  SliderRoot,
  type SliderRootEmits,
  type SliderRootProps,
  SliderThumb,
  SliderTrack,
  useForwardPropsEmits,
} from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<SliderRootProps & { class?: HTMLAttributes["class"] }>();
const emits = defineEmits<SliderRootEmits>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
const forwarded = useForwardPropsEmits(delegated, emits);
</script>

<template>
  <SliderRoot v-bind="forwarded" :class="cn('relative flex w-full touch-none select-none items-center', props.class)">
    <SliderTrack class="relative h-1 w-full grow overflow-hidden rounded-full bg-hover">
      <SliderRange class="absolute h-full bg-accent" />
    </SliderTrack>
    <SliderThumb
      v-for="(_, i) in modelValue ?? [0]"
      :key="i"
      class="block h-3.5 w-3.5 rounded-full border border-accent bg-white shadow transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent"
    />
  </SliderRoot>
</template>
